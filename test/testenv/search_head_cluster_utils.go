package testenv

import (
	"fmt"
	"os/exec"
	"strings"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// DeleteSHC delete Search Head Cluster in given namespace
func DeleteSHC(ns string) {
	output, err := exec.Command("kubectl", "delete", "shc", "-n", ns, "--all").Output()
	if err != nil {
		cmd := fmt.Sprintf("kubectl delete shc -n %s --all", ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
	} else {
		logf.Log.Info("SHC deleted", "Namespace", ns, "stdout", output)
	}
}

// SHCInNamespace returns true if SHC is present in namespace
func SHCInNamespace(ns string) bool {
	output, err := exec.Command("kubectl", "get", "searchheadcluster", "-n", ns).Output()
	deleted := true
	if err != nil {
		cmd := fmt.Sprintf("kubectl get shc -n %s", ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return deleted
	}
	logf.Log.Info("Output of command", "Output", string(output))
	if strings.Contains(string(output), "No resources found in default namespace") {
		deleted = false
	}
	return deleted
}
