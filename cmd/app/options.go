package app

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

func (o Options) AddFlags(fs *pflag.FlagSet) {
	fs.BoolVar(&o.NoDryRun, "no-dry-run", false, "Run the CLI tool _not_ in dry run mode. This will attempt to migrate your cluster.")
	fs.BoolVar(&o.StepAll, "step-all", false, "Run all steps. Cannot be used in conjunction with other step options.")
	fs.BoolVarP(&o.StepInstallCNIs, "step-install-cnis", "1", false, "[1] - install required resource and prepare cluster.")
	fs.BoolVarP(&o.StepRollNodes, "step-roll-nodes", "2", false, "[2] - roll all nodes on the cluster to install both CNIs to workloads.")
	fs.BoolVar(&o.StepMigrateSingleNode, "step-migrate-single-node", false, "[3] - Migrate a single node in the cluster if one is available.")
	fs.BoolVarP(&o.StepMigrateAllNodes, "step-migrate-all-nodes", "3", false, "[3] - Migrate all nodes in the cluster, one by one.")
	fs.BoolVarP(&o.StepCleanUp, "step-clean-up", "4", false, "[4] - Clean up migration resources.")
	fs.StringVarP(&o.LogLevel, "log-level", "v", "info", "Set logging level [debug|info|warn|error|fatal]")
}

func AddKubeFlags(cmd *cobra.Command, fs *pflag.FlagSet) cmdutil.Factory {
	kubeConfigFlags := genericclioptions.NewConfigFlags(true)
	kubeConfigFlags.AddFlags(fs)
	matchVersionKubeConfigFlags := cmdutil.NewMatchVersionFlags(kubeConfigFlags)
	matchVersionKubeConfigFlags.AddFlags(fs)
	factory := cmdutil.NewFactory(matchVersionKubeConfigFlags)

	cmd.Flags().AddGoFlagSet(flag.CommandLine)
	flag.CommandLine.Parse([]string{})
	fakefs := flag.NewFlagSet("fake", flag.ExitOnError)
	klog.InitFlags(fakefs)
	if err := fakefs.Parse([]string{"-logtostderr=false"}); err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	return factory
}

func (o *Options) Validate() error {
	if o.StepMigrateAllNodes && o.StepMigrateSingleNode {
		return errors.New("cannot enable both --step-migrate-all-nodes, as well as --step-migrate-single-node")
	}

	if o.StepAll {
		switch o.StepAll {
		case o.StepInstallCNIs, o.StepRollNodes, o.StepMigrateSingleNode, o.StepMigrateAllNodes, o.StepCleanUp:
			return errors.New("no other step flags may be enabled with --all")
		}
	}

	return nil
}