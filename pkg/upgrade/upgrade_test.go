package upgrade

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	harvv1api "github.com/harvester/harvester/pkg/apis/harvesterhci.io/v1beta1"
	rancherv3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	"github.com/rancher/wrangler/v3/pkg/condition"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/e2e-framework/klient/k8s"
	"sigs.k8s.io/e2e-framework/klient/wait"
	"sigs.k8s.io/e2e-framework/klient/wait/conditions"
	"sigs.k8s.io/e2e-framework/pkg/env"
	"sigs.k8s.io/e2e-framework/pkg/envconf"
	"sigs.k8s.io/e2e-framework/pkg/features"
)

const (
	harvesterSystemNamespace = "harvester-system"
	managedChartNamespace    = "fleet-local"
)

var (
	testenv    env.Environment
	testAnswer TestAnswer

	upgradeImageName string
	upgradeName      string
)

type TestAnswer struct {
	ControllerCount int `yaml:"controllerCount,omitempty"`
	EtcdCount       int `yaml:"etcdCount,omitempty"`
}

func TestMain(m *testing.M) {
	data, err := os.ReadFile("/home/kiefer/codes/try-harvester/test_answer.yaml")
	if err != nil {
		fmt.Println("fail to read answer file: ", err)
		os.Exit(1)
	}
	err = yaml.Unmarshal(data, &testAnswer)
	if err != nil {
		fmt.Println("fail to unmarshal answer file", err)
		os.Exit(1)
	}
	testenv = env.NewWithKubeConfig("/home/kiefer/codes/try-harvester/kubeconfig")

	r := testenv.EnvConf().Client().Resources()
	err = harvv1api.AddToScheme(r.GetScheme())
	if err != nil {
		fmt.Println("fail to add to scheme: ", err)
		os.Exit(1)
	}

	err = rancherv3.AddToScheme(r.GetScheme())
	if err != nil {
		fmt.Println("fail to add to scheme: ", err)
		os.Exit(1)
	}
	os.Exit(testenv.Run(m))
}

func createVersion(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	r := cfg.Client().Resources()

	version := &harvv1api.Version{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: harvesterSystemNamespace,
			Name:      "v8.8.8",
		},
		Spec: harvv1api.VersionSpec{
			ISOURL: "http://10.10.0.1/harvester/harvester.iso",
		},
	}

	err := r.Create(ctx, version)
	if err != nil {
		t.Fatalf("fail to create version: %v", err)
	}

	return ctx
}

func createUpgradeImage(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	r := cfg.Client().Resources()

	upgradeImage := &harvv1api.VirtualMachineImage{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    harvesterSystemNamespace,
			GenerateName: "upgrade-image-",
			Labels: map[string]string{
				"harvesterhci.io/upgrade": "true",
			},
		},
		Spec: harvv1api.VirtualMachineImageSpec{
			DisplayName: "upgrade-iso",
			URL:         "http://10.10.0.1/harvester/harvester.iso",
			SourceType:  "download",
			StorageClassParameters: map[string]string{
				"mirroring":           "true",
				"numberOfReplicas":    "2",
				"staleReplicaTimeout": "30",
			},
		},
	}

	err := r.Create(ctx, upgradeImage)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("create and wait for upgrade image: %s", upgradeImage.Name)
	upgradeImageName = fmt.Sprintf("%s/%s", upgradeImage.Namespace, upgradeImage.Name)
	err = wait.For(conditions.New(cfg.Client().Resources(harvesterSystemNamespace)).ResourceMatch(
		upgradeImage,
		func(object k8s.Object) bool {
			img := object.(*harvv1api.VirtualMachineImage)
			return harvv1api.ImageImported.IsTrue(img)
		},
	), wait.WithImmediate(), wait.WithInterval(10*time.Second), wait.WithTimeout(5*time.Minute))

	if err != nil {
		t.Fatal(err)
	}

	return ctx
}

func createUpgrade(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	r := cfg.Client().Resources()

	if upgradeImageName == "" {
		t.Fatal("upgrade image name is not set")
	}

	upgrade := &harvv1api.Upgrade{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "hvst-upgrade-",
			Namespace:    harvesterSystemNamespace,
		},
		Spec: harvv1api.UpgradeSpec{
			Version:    "v8.8.8",
			Image:      upgradeImageName,
			LogEnabled: true,
		},
	}
	err := r.Create(ctx, upgrade)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("upgrade is created: %s", upgrade.Name)

	upgradeName = upgrade.Name

	return ctx
}

func wait_upgrade_condition(ctx context.Context, t *testing.T, cfg *envconf.Config, interval, timeout time.Duration, cond condition.Cond) context.Context {
	r := cfg.Client().Resources()

	var upgrade harvv1api.Upgrade
	err := r.Get(ctx, upgradeName, harvesterSystemNamespace, &upgrade)
	if err != nil {
		t.Fatalf("fail to get upgrade: %v", err)
	}

	t.Logf("wait for upgrade status condition: %s, interval: %s, timeout: %s", cond, interval, timeout)
	err = wait.For(conditions.New(cfg.Client().Resources(harvesterSystemNamespace)).ResourceMatch(
		&upgrade,
		func(object k8s.Object) bool {
			u := object.(*harvv1api.Upgrade)

			if harvv1api.UpgradeCompleted.IsFalse(u) {
				t.Fatalf("upgrade failed: %v", u.Status)
			}
			return cond.IsTrue(u)
		},
	), wait.WithImmediate(), wait.WithInterval(interval), wait.WithTimeout(timeout))

	if err != nil {
		t.Fatalf("Error on waiting upgrade status condition: %s, error: %v", cond, err)
	}
	return ctx
}

func wait_image(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	return wait_upgrade_condition(ctx, t, cfg, 10*time.Second, 5*time.Minute, harvv1api.ImageReady)
}

func wait_log(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	return wait_upgrade_condition(ctx, t, cfg, 10*time.Second, 5*time.Minute, harvv1api.LogReady)
}

func wait_repo(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	return wait_upgrade_condition(ctx, t, cfg, 10*time.Second, 10*time.Minute, harvv1api.RepoProvisioned)
}

func wait_node_prepared(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	return wait_upgrade_condition(ctx, t, cfg, 30*time.Second, 30*time.Minute, harvv1api.NodesPrepared)
}

func wait_system_service_upgraded(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	return wait_upgrade_condition(ctx, t, cfg, 30*time.Second, 30*time.Minute, harvv1api.SystemServicesUpgraded)
}

func wait_nodes_upgraded(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	return wait_upgrade_condition(ctx, t, cfg, 30*time.Second, 45*time.Minute, harvv1api.NodesUpgraded)
}

func wait_upgraded_complete(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	return wait_upgrade_condition(ctx, t, cfg, 10*time.Second, 3*time.Minute, harvv1api.UpgradeCompleted)
}

func wait_managed_charts(ctx context.Context, t *testing.T, cfg *envconf.Config) context.Context {
	var managedCharts rancherv3.ManagedChartList
	err := cfg.Client().Resources(managedChartNamespace).List(ctx, &managedCharts)
	if err != nil {
		t.Fatal(err)
	}

	// wait until all managed charts are ready
	err = wait.For(conditions.New(cfg.Client().Resources(managedChartNamespace)).ResourceListMatchN(
		&managedCharts,
		len(managedCharts.Items),
		func(object k8s.Object) bool {
			m := object.(*rancherv3.ManagedChart)
			ready := rancherv3.Ready.IsTrue(m)
			if !ready {
				t.Logf("managed chart: %s is not ready", m.Name)
			}
			return ready
		},
	), wait.WithImmediate(), wait.WithInterval(10*time.Second), wait.WithTimeout(5*time.Minute))

	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func TestHarvesterUpgrade(t *testing.T) {
	f1 := features.New("upgrade").
		WithLabel("type", "upgrade").
		Assess("wait managed charts", wait_managed_charts).
		Assess("create an upgrade image", createUpgradeImage).
		Assess("create an upgrade", createUpgrade).
		Assess("wait log", wait_log).
		Assess("wait image", wait_image).
		Assess("wait repo", wait_repo).
		Assess("wait node prepared", wait_node_prepared).
		Assess("wait system services upgraded", wait_system_service_upgraded).
		Assess("wait nodes upgraded", wait_nodes_upgraded).
		Assess("wait upgrade complete", wait_upgraded_complete).
		Feature()

	testenv.Test(t, f1)
}
