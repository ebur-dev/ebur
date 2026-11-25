package startup

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

// runKey is HKCU\<runKey>; tests point it elsewhere.
var runKey = `Software\Microsoft\Windows\CurrentVersion\Run`

func read() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer k.Close()
	v, _, err := k.GetStringValue(ValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	return v, err
}

func write(cmd string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(ValueName, cmd)
}

func remove() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(ValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
