package ports

import (
	"encoding/binary"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetExtendedTcpTable = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")

const (
	tcpTableOwnerPIDListener = 3 // TCP_TABLE_OWNER_PID_LISTENER
	afInet                   = 2
	afInet6                  = 23
	row4Size                 = 24 // MIB_TCPROW_OWNER_PID
	row6Size                 = 56 // MIB_TCP6ROW_OWNER_PID
)

// findListener looks up the listening-socket tables for IPv4 and IPv6.
func findListener(port int) (Owner, bool, error) {
	for _, fam := range []struct {
		af, rowSize, portOff, pidOff int
	}{
		{afInet, row4Size, 8, 20},
		{afInet6, row6Size, 20, 52},
	} {
		table, err := tcpTable(fam.af)
		if err != nil {
			return Owner{}, false, err
		}
		if len(table) < 4 {
			continue
		}
		n := int(binary.LittleEndian.Uint32(table[:4]))
		for i := 0; i < n; i++ {
			off := 4 + i*fam.rowSize
			if off+fam.rowSize > len(table) {
				break
			}
			row := table[off : off+fam.rowSize]
			// dwLocalPort holds the port in network byte order in its low 16 bits.
			p := int(row[fam.portOff])<<8 | int(row[fam.portOff+1])
			if p == port {
				pid := int(binary.LittleEndian.Uint32(row[fam.pidOff:]))
				return describe(pid), true, nil
			}
		}
	}
	return Owner{}, false, nil
}

func tcpTable(af int) ([]byte, error) {
	size := uint32(16 * 1024)
	for attempt := 0; attempt < 5; attempt++ {
		buf := make([]byte, size)
		r, _, _ := procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)),
			0, uintptr(af), tcpTableOwnerPIDListener, 0)
		switch windows.Errno(r) {
		case 0:
			return buf[:size], nil
		case windows.ERROR_INSUFFICIENT_BUFFER:
			size += 4096 // table grew between calls
			continue
		default:
			return nil, windows.Errno(r)
		}
	}
	return nil, windows.ERROR_INSUFFICIENT_BUFFER
}

func describe(pid int) Owner {
	switch pid {
	case 0:
		return Owner{PID: 0, Name: "System Idle Process"}
	case 4:
		return Owner{PID: 4, Name: "System (http.sys)"}
	}
	o := Owner{PID: pid, Name: "unknown program"}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return o // e.g. a process owned by another user without admin rights
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err == nil {
		o.Path = windows.UTF16ToString(buf[:n])
		o.Name = filepath.Base(o.Path)
	}
	return o
}
