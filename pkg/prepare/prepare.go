package prepare

import (
	"context"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/timescale/cni-migration/pkg"
	"github.com/timescale/cni-migration/pkg/config"
	"github.com/timescale/cni-migration/pkg/util"
)

var _ pkg.Step = &Prepare{}

type Prepare struct {
	ctx context.Context
	log *logrus.Entry

	config  *config.Config
	client  *kubernetes.Clientset
	factory *util.Factory
}

func New(ctx context.Context, config *config.Config) pkg.Step {
	log := config.Log.WithField("step", "1-prepare")
	return &Prepare{
		log:     log,
		ctx:     ctx,
		config:  config,
		client:  config.Client,
		factory: util.New(ctx, log, config.Client),
	}
}

// Ready ensures that
// - Nodes have correct labels
// - The required resources exist
// - Calico DaemonSet has been patched
func (p *Prepare) Ready() (bool, error) {
	nodes, err := p.factory.GetMasterNodes()
	if err != nil {
		return false, err
	}

	for _, n := range nodes.Items {
		if !p.hasRequiredLabel(n.Labels) {
			return false, nil
		}
	}

	patched, err := p.calicoIsPatched()
	if err != nil || !patched {
		return false, err
	}

	requiredResources, err := p.factory.Has(p.config.WatchedResources)
	if err != nil || !requiredResources {
		return false, err
	}

	p.log.Info("step 1 ready")

	return true, nil
}

// Run will ensure that
// - Node have correct labels
// - The required resources exist
// - Calico DaemonSet has been patched
func (p *Prepare) Run(dryrun bool) error {
	p.log.Infof("preparing migration...")

	nodes, err := p.factory.GetMasterNodes()
	if err != nil {
		return err
	}

	for _, n := range nodes.Items {
		if !p.hasRequiredLabel(n.Labels) {
			p.log.Infof("updating label on node %s", n.Name)

			if dryrun {
				continue
			}

			delete(n.Labels, p.config.Labels.Cilium)
			delete(n.Labels, p.config.Labels.CNIPriorityCilium)

			n.Labels[p.config.Labels.CalicoCilium] = p.config.Labels.Value
			n.Labels[p.config.Labels.CNIPriorityCalico] = p.config.Labels.Value

			_, err := p.client.CoreV1().Nodes().Update(p.ctx, n.DeepCopy(), metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}

	// Idempotency. Check to see if Calico is patched. If it is, we skip patching it.
	patched, err := p.calicoIsPatched()
	if err != nil {
		return err
	}

	if !patched {
		p.log.Infof("patching calico DaemonSet with node selector %s=%s",
			p.config.Labels.CalicoCilium, p.config.Labels.Value)

		if !dryrun {
			if err := p.patchCalico(); err != nil {
				return err
			}
		}
	}

	requiredResources, err := p.factory.Has(p.config.WatchedResources)
	if err != nil {
		return err
	}

	if !requiredResources {
		p.log.Infof("creating cilium resources")
		if !dryrun {
			if err := p.factory.CreateDaemonSet(p.config.Paths.Cilium, "kube-system", "cilium"); err != nil {
				return err
			}
		}

		p.log.Infof("creating multus resources")
		if !dryrun {
			if err := p.factory.CreateDaemonSet(p.config.Paths.Multus, "kube-system", "kube-multus-calico"); err != nil {
				return err
			}
		}
	}

	if !dryrun {
		if err := p.factory.WaitAllReady(p.config.WatchedResources); err != nil {
			return err
		}

		if err := p.factory.CheckKnetStress(); err != nil {
			return err
		}
	}

	return nil
}

func (p *Prepare) patchCalico() error {
	ds, err := p.client.AppsV1().DaemonSets("kube-system").Get(p.ctx, "calico-node", metav1.GetOptions{})
	if err != nil {
		return err
	}

	if ds.Spec.Template.Spec.NodeSelector == nil {
		ds.Spec.Template.Spec.NodeSelector = make(map[string]string)
	}
	ds.Spec.Template.Spec.NodeSelector[p.config.Labels.CalicoCilium] = p.config.Labels.Value

	_, err = p.client.AppsV1().DaemonSets("kube-system").Update(p.ctx, ds, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (p *Prepare) hasRequiredLabel(labels map[string]string) bool {
	if labels == nil {
		return false
	}

	_, cclOK := labels[p.config.Labels.CalicoCilium]
	_, clOK := labels[p.config.Labels.Cilium]

	_, prioCal := labels[p.config.Labels.CNIPriorityCalico]
	_, prioCil := labels[p.config.Labels.CNIPriorityCilium]
	_, migrated := labels[p.config.Labels.Migrated]

	// If both true, or both false, does not have correct labels
	if cclOK == clOK {
		return false
	}

	var onlyOne bool
	for _, b := range []bool{
		prioCal, prioCil, migrated,
	} {
		if b {
			if onlyOne {
				return false
			}

			onlyOne = true
		}
	}

	if !onlyOne {
		return false
	}

	return true
}

func (p *Prepare) calicoIsPatched() (bool, error) {
	ds, err := p.client.AppsV1().DaemonSets("kube-system").Get(p.ctx, "calico-node", metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	if ds.Spec.Template.Spec.NodeSelector == nil {
		return false, nil
	}
	if v, ok := ds.Spec.Template.Spec.NodeSelector[p.config.Labels.CalicoCilium]; !ok || v != p.config.Labels.Value {
		return false, nil
	}

	return true, nil
}
