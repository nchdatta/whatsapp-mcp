package secret

import (
	"errors"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// On Windows the master key is encrypted with DPAPI, so only this Windows
// account on this computer can decrypt it.

func loadMaster(dataDir string) ([]byte, error) {
	data, err := os.ReadFile(keyFile(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNoKey
	}
	if err != nil {
		return nil, err
	}
	return dpapi(data, false)
}

func saveMaster(dataDir string, key []byte) error {
	data, err := dpapi(key, true)
	if err != nil {
		return err
	}
	return os.WriteFile(keyFile(dataDir), data, 0o600)
}

func deleteMaster(dataDir string) error {
	err := os.Remove(keyFile(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func dpapi(data []byte, protect bool) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("empty key")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	if protect {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte{}, unsafe.Slice(out.Data, out.Size)...), nil
}
