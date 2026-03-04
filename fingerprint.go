package sdk

import (
	"crypto/sha256"
	"fmt"
	"net"
	"runtime"
	"strings"

	"github.com/denisbrodbeck/machineid"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
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

	if infos, err := cpu.Info(); err == nil && len(infos) > 0 {
		aux["cpu_model"] = infos[0].ModelName
		aux["cpu_cores"] = fmt.Sprintf("%d", infos[0].Cores)
	}

	if vmem, err := mem.VirtualMemory(); err == nil {
		aux["total_ram_mb"] = fmt.Sprintf("%d", vmem.Total/1024/1024)
	}

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
