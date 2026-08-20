//go:build bof && (!windows || 386)

package bofloader

type unwindRegistration struct{}

func registerUnwindInfo(_ *objectFile, _ *memoryRegion) (*unwindRegistration, error) {
	return nil, nil
}

func (*unwindRegistration) close() error {
	return nil
}
