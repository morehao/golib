package gconc

import "fmt"

// errFromRecover 将 recover 得到的值转换为 error。
func errFromRecover(r any) error {
	switch v := r.(type) {
	case nil:
		return nil
	case string:
		return fmt.Errorf("gconc unknown panic: %s", v)
	case error:
		return v
	default:
		return fmt.Errorf("gconc unknown panic: %v", v)
	}
}
