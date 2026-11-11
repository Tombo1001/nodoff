//go:build windows

package ddc

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// All DLLs come from System32 only (NewLazySystemDLL), never the app folder.
var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	dxva2  = windows.NewLazySystemDLL("dxva2.dll")

	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfo      = user32.NewProc("GetMonitorInfoW")
	procEnumDisplayDevices  = user32.NewProc("EnumDisplayDevicesW")
	procNumPhysical         = dxva2.NewProc("GetNumberOfPhysicalMonitorsFromHMONITOR")
	procGetPhysical         = dxva2.NewProc("GetPhysicalMonitorsFromHMONITOR")
	procDestroyPhysical     = dxva2.NewProc("DestroyPhysicalMonitors")
	procGetVCP              = dxva2.NewProc("GetVCPFeatureAndVCPFeatureReply")
	procSetVCP              = dxva2.NewProc("SetVCPFeature")
	procCapsLength          = dxva2.NewProc("GetCapabilitiesStringLength")
	procCapsRequestAndReply = dxva2.NewProc("CapabilitiesRequestAndCapabilitiesReply")
	enumCallback            = windows.NewCallback(enumProc) // created once: callbacks are never freed
	enumMu                  sync.Mutex
	enumFound               []uintptr
)

const (
	monitorInfoPrimary        = 1
	eddGetDeviceInterfaceName = 1
	displayDeviceActive       = 1
)

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfoEx struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
	Device  [32]uint16
}

type displayDevice struct {
	Size       uint32
	Name       [32]uint16
	String     [128]uint16
	StateFlags uint32
	ID         [128]uint16
	Key        [128]uint16
}

type physicalMonitor struct {
	Handle      windows.Handle
	Description [128]uint16
}

// Monitor is one physical monitor, open for DDC/CI until the Session closes.
type Monitor struct {
	ID          string // stable across reboots and cable swaps: maker, product and serial
	Name        string // from the EDID, e.g. "LEN L28u-30"
	Serial      string
	Year        int
	Display     string // GDI name, e.g. \\.\DISPLAY1
	Instance    string // Windows device instance ID
	Primary     bool
	Left, Top   int32
	Width       int32
	Height      int32
	Description string // what dxva2 calls it, usually "Generic PnP Monitor"
	h           windows.Handle
}

// Session holds the physical monitor handles from one enumeration. Handles go stale
// when the display layout changes, so open a fresh Session for each piece of work.
type Session struct {
	Monitors []*Monitor
	handles  [][]physicalMonitor
}

// sessionMu serialises all DDC/CI traffic. Monitors drop or garble messages that overlap.
var sessionMu sync.Mutex

// Open enumerates monitors and returns a Session. Close it when done. Only one Session
// is open at a time; Open waits for the previous one to close.
func Open() (*Session, error) {
	sessionMu.Lock()
	s, err := open()
	if err != nil {
		sessionMu.Unlock()
		return nil, err
	}
	return s, nil
}

// Close releases the handles.
func (s *Session) Close() {
	for _, pm := range s.handles {
		if len(pm) > 0 {
			procDestroyPhysical.Call(uintptr(len(pm)), uintptr(unsafe.Pointer(&pm[0])))
		}
	}
	s.handles = nil
	s.Monitors = nil
	sessionMu.Unlock()
}

func enumProc(hm, hdc, r, lparam uintptr) uintptr {
	enumFound = append(enumFound, hm)
	return 1
}

func open() (*Session, error) {
	if err := dxva2.Load(); err != nil {
		return nil, fmt.Errorf("dxva2.dll: %w", err)
	}
	enumMu.Lock()
	enumFound = enumFound[:0]
	r, _, err := procEnumDisplayMonitors.Call(0, 0, enumCallback, 0)
	hmons := append([]uintptr(nil), enumFound...)
	enumMu.Unlock()
	if r == 0 {
		return nil, fmt.Errorf("EnumDisplayMonitors: %w", err)
	}
	s := &Session{}
	for _, hm := range hmons {
		mi := monitorInfoEx{Size: uint32(unsafe.Sizeof(monitorInfoEx{}))}
		if r, _, _ := procGetMonitorInfo.Call(hm, uintptr(unsafe.Pointer(&mi))); r == 0 {
			continue
		}
		var n uint32
		if r, _, _ := procNumPhysical.Call(hm, uintptr(unsafe.Pointer(&n))); r == 0 || n == 0 || n > 16 {
			continue
		}
		pm := make([]physicalMonitor, n)
		if r, _, _ := procGetPhysical.Call(hm, uintptr(n), uintptr(unsafe.Pointer(&pm[0]))); r == 0 {
			continue
		}
		s.handles = append(s.handles, pm)
		gdi := windows.UTF16ToString(mi.Device[:])
		paths := activeMonitorPaths(gdi)
		for i := range pm {
			m := &Monitor{
				Display:     gdi,
				Primary:     mi.Flags&monitorInfoPrimary != 0,
				Left:        mi.Monitor.Left,
				Top:         mi.Monitor.Top,
				Width:       mi.Monitor.Right - mi.Monitor.Left,
				Height:      mi.Monitor.Bottom - mi.Monitor.Top,
				Description: windows.UTF16ToString(pm[i].Description[:]),
				h:           pm[i].Handle,
			}
			// Physical monitors come back in the same order as the adapter's active
			// monitors. That's the only link Windows gives between the two lists.
			if i < len(paths) {
				m.Instance = InstanceFromInterface(paths[i])
			}
			m.identify()
			s.Monitors = append(s.Monitors, m)
		}
	}
	sort.SliceStable(s.Monitors, func(a, b int) bool {
		if s.Monitors[a].Left != s.Monitors[b].Left {
			return s.Monitors[a].Left < s.Monitors[b].Left
		}
		return s.Monitors[a].Top < s.Monitors[b].Top
	})
	return s, nil
}

// activeMonitorPaths lists the device interface paths of the monitors attached to a
// GDI display such as \\.\DISPLAY1.
func activeMonitorPaths(gdi string) []string {
	name, err := windows.UTF16PtrFromString(gdi)
	if err != nil {
		return nil
	}
	var out []string
	for i := uint32(0); i < 16; i++ {
		dd := displayDevice{Size: uint32(unsafe.Sizeof(displayDevice{}))}
		r, _, _ := procEnumDisplayDevices.Call(uintptr(unsafe.Pointer(name)), uintptr(i), uintptr(unsafe.Pointer(&dd)), eddGetDeviceInterfaceName)
		if r == 0 {
			break
		}
		if dd.StateFlags&displayDeviceActive != 0 {
			out = append(out, windows.UTF16ToString(dd.ID[:]))
		}
	}
	return out
}

// identify reads the EDID from the registry to get a stable ID, name and serial.
func (m *Monitor) identify() {
	fallback := m.Instance
	if fallback == "" {
		fallback = m.Display
	}
	m.ID = Slug("mon_" + fallback)
	m.Name = m.Description
	if m.Instance == "" {
		return
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Enum\`+m.Instance+`\Device Parameters`, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	raw, _, err := k.GetBinaryValue("EDID")
	if err != nil {
		return
	}
	e, err := ParseEDID(raw)
	if err != nil {
		return
	}
	m.ID = e.ID(m.Instance)
	m.Serial = e.Serial
	m.Year = e.Year
	if e.Name != "" {
		m.Name = e.Name
	}
}

// ErrNoReply means the monitor didn't answer, which is common: DDC/CI may be off in
// the monitor's menu, or the monitor is asleep in a state that ignores DDC.
var ErrNoReply = errors.New("the monitor didn't answer over DDC/CI")

// Get reads a VCP code. It retries, because DDC/CI drops messages now and then.
func (m *Monitor) Get(code byte) (cur, max uint32, err error) {
	for try := 0; try < 3; try++ {
		var typ uint32
		r, _, _ := procGetVCP.Call(uintptr(m.h), uintptr(code), uintptr(unsafe.Pointer(&typ)),
			uintptr(unsafe.Pointer(&cur)), uintptr(unsafe.Pointer(&max)))
		if r != 0 {
			return cur, max, nil
		}
		time.Sleep(60 * time.Millisecond)
	}
	return 0, 0, ErrNoReply
}

// Set writes a VCP code.
func (m *Monitor) Set(code byte, value uint32) error {
	for try := 0; try < 2; try++ {
		if r, _, _ := procSetVCP.Call(uintptr(m.h), uintptr(code), uintptr(value)); r != 0 {
			// MCCS asks hosts to wait 50 ms after a write before the next message.
			time.Sleep(60 * time.Millisecond)
			return nil
		}
		time.Sleep(60 * time.Millisecond)
	}
	return ErrNoReply
}

// Capabilities asks the monitor for its MCCS capabilities string. It's slow (up to a
// couple of seconds), so callers cache it.
func (m *Monitor) Capabilities() (Caps, error) {
	var n uint32
	if r, _, _ := procCapsLength.Call(uintptr(m.h), uintptr(unsafe.Pointer(&n))); r == 0 || n == 0 {
		return Caps{}, ErrNoReply
	}
	if n > 8192 {
		return Caps{}, fmt.Errorf("capabilities string too long (%d bytes)", n)
	}
	buf := make([]byte, n+1)
	if r, _, _ := procCapsRequestAndReply.Call(uintptr(m.h), uintptr(unsafe.Pointer(&buf[0])), uintptr(n)); r == 0 {
		return Caps{}, ErrNoReply
	}
	for i, b := range buf {
		if b == 0 {
			buf = buf[:i]
			break
		}
	}
	return ParseCaps(string(buf)), nil
}
