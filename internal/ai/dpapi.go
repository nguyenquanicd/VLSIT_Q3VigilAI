package ai

import (
	"encoding/base64"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Protect encrypts a secret with Windows DPAPI for the current user. The
// result is only readable by the same Windows account on the same machine,
// so a copied data folder does not leak the key.
func Protect(secret string) (string, error) {
	if secret == "" {
		return "", nil
	}
	data := []byte(secret)
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, out.Size)), nil
}

// Unprotect reverses Protect.
func Unprotect(stored string) (string, error) {
	if stored == "" {
		return "", nil
	}
	data, err := base64.StdEncoding.DecodeString(stored)
	if err != nil || len(data) == 0 {
		return "", errors.New("khóa API lưu trữ bị hỏng")
	}
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", errors.New("không giải mã được khóa API (có thể thư mục dữ liệu được chép từ máy hoặc tài khoản khác); hãy nhập lại khóa")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}
