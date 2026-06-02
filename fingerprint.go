package sdk

import (
	"crypto/sha256"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"

	"github.com/denisbrodbeck/machineid"
)

type Fingerprint struct {
	machineID  string
	auxSignals map[string]string
}

func collectFingerprint() (*Fingerprint, error) {
	mid, err := machineid.ProtectedID("deploy-guard")
	if err != nil {
		return nil, fmt.Errorf("collect machine id: %w", err)
	}

	hash := sha256.Sum256([]byte(mid))
	hashedID := fmt.Sprintf("sha256:%x", hash)

	aux := make(map[string]string)
	aux["os"] = runtime.GOOS
	aux["arch"] = runtime.GOARCH

	populateCPUInfo(aux)
	populateMemoryInfo(aux)

	if macs := getMACAddresses(); len(macs) > 0 {
		aux["mac_addresses"] = strings.Join(macs, ",")
	}

	return &Fingerprint{machineID: hashedID, auxSignals: aux}, nil
}

func (f *Fingerprint) MachineID() string {
	return f.machineID
}

func (f *Fingerprint) AuxSignals() map[string]string {
	return f.auxSignals
}

func getMACAddresses() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var macs []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		if iface.HardwareAddr == nil {
			continue
		}
		mac := iface.HardwareAddr.String()
		if mac != "" {
			macs = append(macs, mac)
		}
	}
	return macs
}

func populateCPUInfo(aux map[string]string) {
	switch runtime.GOOS {
	case "darwin":
		if model, err := runCommand("sysctl", "-n", "machdep.cpu.brand_string"); err == nil && model != "" {
			aux["cpu_model"] = model
		}
		if cores, err := runCommand("sysctl", "-n", "hw.physicalcpu"); err == nil && cores != "" {
			aux["cpu_cores"] = cores
		}
	default:
		if cores, err := runCommand("getconf", "_NPROCESSORS_ONLN"); err == nil && cores != "" {
			aux["cpu_cores"] = cores
		}
	}
}

func populateMemoryInfo(aux map[string]string) {
	switch runtime.GOOS {
	case "darwin":
		if bytes, err := runCommand("sysctl", "-n", "hw.memsize"); err == nil && bytes != "" {
			aux["total_ram_mb"] = bytesToMBString(bytes)
		}
	}
}

func runCommand(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func bytesToMBString(value string) string {
	var bytes uint64
	if _, err := fmt.Sscanf(strings.TrimSpace(value), "%d", &bytes); err != nil || bytes == 0 {
		return ""
	}
	return fmt.Sprintf("%d", bytes/1024/1024)
}
