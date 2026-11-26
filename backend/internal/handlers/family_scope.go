package handlers

import (
	"yihe-care/internal/auth"
	"yihe-care/internal/session"
)

// familyPhoneOK 归属隔离：ADMIN 直通；FAMILY 只能操作自己的手机号。
func familyPhoneOK(p *session.Principal, phone string) bool {
	if p == nil {
		return false
	}
	if p.Role == auth.RoleAdmin {
		return true
	}
	return p.Role == auth.RoleFamily && phone != "" && phone == p.Phone
}

// Zero leaves the administrator's explicit phone lookup unrestricted.
func familySelfID(p *session.Principal) int64 {
	if p != nil && p.Role == auth.RoleFamily {
		return p.ID
	}
	return 0
}
