//go:build linux && !android && (386 || amd64 || (arm && arm.7) || arm64 || ppc64le || riscv64)

package memmod

import (
	"debug/elf"
	"fmt"
)

func initLinuxDynAPI() error {
	if err := requireDynamicELFHost(); err != nil {
		return err
	}
	modules, err := runtimeModules()
	if err != nil {
		return err
	}

	dlopenAddr, err := resolveRuntimeAPISymbol(modules, "dlopen")
	if err != nil {
		return fmt.Errorf("resolve runtime symbol dlopen: %w", err)
	}
	dlsymAddr, err := resolveRuntimeAPISymbol(modules, "dlsym")
	if err != nil {
		return fmt.Errorf("resolve runtime symbol dlsym: %w", err)
	}
	dlerrorAddr, err := resolveRuntimeAPISymbol(modules, "dlerror")
	if err != nil {
		return fmt.Errorf("resolve runtime symbol dlerror: %w", err)
	}
	dlvsymAddr, _ := resolveRuntimeAPISymbol(modules, "dlvsym")
	dlcloseAddr, _ := resolveRuntimeAPISymbol(modules, "dlclose")

	linuxAPI = linuxDynAPI{
		dlopen:        dlopenAddr,
		dlsym:         dlsymAddr,
		dlvsym:        dlvsymAddr,
		dlclose:       dlcloseAddr,
		dlerror:       dlerrorAddr,
		defaultHandle: 0,
	}
	return nil
}

func requireDynamicELFHost() error {
	host, err := elf.Open("/proc/self/exe")
	if err != nil {
		return fmt.Errorf("inspect Linux host executable: %w", err)
	}
	defer host.Close()
	for _, program := range host.Progs {
		if program.Type == elf.PT_INTERP {
			return nil
		}
	}
	return errStaticELFHost
}
