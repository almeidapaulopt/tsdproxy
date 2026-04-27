// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package consts

const (
	PermNone         = 0o000 // No permissions
	PermOwnerRead    = 0o400 // Owner: Read
	PermOwnerWrite   = 0o200 // Owner: Write
	PermOwnerExecute = 0o100 // Owner: Execute
	PermOwnerAll     = 0o700 // Owner: Read, Write, Execute

	PermGroupRead    = 0o040 // Group: Read
	PermGroupWrite   = 0o020 // Group: Write
	PermGroupExecute = 0o010 // Group: Execute
	PermGroupAll     = 0o070 // Group: Read, Write, Execute

	PermOthersRead    = 0o004 // Others: Read
	PermOthersWrite   = 0o002 // Others: Write
	PermOthersExecute = 0o001 // Others: Execute
	PermOthersAll     = 0o007 // Others: Read, Write, Execute

	PermOwnerGroupRead = 0o440 // Owner and Group: Read
	PermOwnerGroupAll  = 0o770 // Owner and Group: All

	PermAllRead    = 0o444 // Everyone: Read
	PermAllWrite   = 0o222 // Everyone: Write
	PermAllExecute = 0o111 // Everyone: Execute
	PermAll        = 0o777 // Everyone: Read, Write, Execute
)
