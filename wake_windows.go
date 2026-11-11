//go:build windows

package main

// Two conveniences for when the screens are asleep:
//
//   - lock Windows a set number of minutes after every screen has gone to sleep;
//   - wake the screens from the keyboard with a double tap of Ctrl.
//
// Both follow the power commands nodoff itself has sent, not what the monitors report,
// because a sleeping monitor may not answer over DDC/CI.

import (
	"log"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procLockWorkStation     = user32.NewProc("LockWorkStation")
	procGetLastInputInfo    = user32.NewProc("GetLastInputInfo")
	procOpenInputDesktop    = user32.NewProc("OpenInputDesktop")
	procCloseDesktop        = user32.NewProc("CloseDesktop")
	procGetUserObjectInfo   = user32.NewProc("GetUserObjectInformationW")
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessage          = user32.NewProc("GetMessageW")
	procPeekMessage         = user32.NewProc("PeekMessageW")
	procPostThreadMessage   = user32.NewProc("PostThreadMessageW")
	wakeRequests            = make(chan string, 1)
	pwrMu                   sync.Mutex
	asleep                  = map[string]bool{} // monitors nodoff sent to sleep and hasn't woken since
	lockTimer               *time.Timer
	watchingLocked          bool
)

func init() {
	go func() {
		for why := range wakeRequests {
			log.Printf("%s: waking the screens", why)
			mons.allPower(true)
		}
	}()
}

func requestWake(why string) {
	select {
	case wakeRequests <- why:
	default:
	}
}

// notePower records a power command that the monitor accepted.
func notePower(id string, on bool) {
	pwrMu.Lock()
	defer pwrMu.Unlock()
	if on {
		delete(asleep, id)
		if lockTimer != nil {
			lockTimer.Stop()
			lockTimer = nil
			log.Printf("a screen woke, so the PC won't be locked")
		}
		return
	}
	asleep[id] = true
	set := store.Get()
	if set.WakeDoubleCtrl && !watchingLocked {
		watchingLocked = true
		go watchLockedInput()
	}
	if set.LockAfterOff && lockTimer == nil && allAsleep() {
		lockTimer = time.AfterFunc(time.Duration(set.LockAfterMinutes)*time.Minute, lockIfStillAsleep)
		log.Printf("every screen is asleep: locking the PC in %d min unless one wakes", set.LockAfterMinutes)
	}
}

// allAsleep is true when nodoff has put every connected, power-capable monitor to
// sleep. Call it with pwrMu held.
func allAsleep() bool {
	n := 0
	for _, v := range mons.views() {
		if !v.Present || !v.CanPower {
			continue
		}
		if !asleep[v.ID] {
			return false
		}
		n++
	}
	return n > 0
}

func lockIfStillAsleep() {
	pwrMu.Lock()
	lockTimer = nil
	ok := store.Get().LockAfterOff && allAsleep()
	pwrMu.Unlock()
	if !ok || sessionLocked() {
		return
	}
	if r, _, err := procLockWorkStation.Call(); r == 0 {
		log.Printf("couldn't lock the PC: %v", err)
		return
	}
	log.Printf("locked the PC: the screens stayed asleep")
}

// ---------- waking while Windows is locked ----------

// watchLockedInput runs while any screen is asleep. The lock screen is a separate
// desktop that hides key presses from every app, so the Ctrl hook is deaf there.
// Windows still records when the last input happened, without saying what it was,
// so on the lock screen any key or mouse movement wakes the screens.
func watchLockedInput() {
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	var wasLocked bool
	var base uint32
	var since time.Time
	for range t.C {
		pwrMu.Lock()
		if len(asleep) == 0 || !store.Get().WakeDoubleCtrl {
			watchingLocked = false
			pwrMu.Unlock()
			return
		}
		pwrMu.Unlock()
		locked, tick := sessionLocked(), lastInputTick()
		if locked != wasLocked {
			wasLocked, base, since = locked, tick, time.Now()
			continue
		}
		if locked && tick != base {
			base = tick
			// Skip the first moments, which catch the keys that locked the PC being released.
			if time.Since(since) > 3*time.Second {
				requestWake("input on the lock screen")
			}
		}
	}
}

// sessionLocked reports whether the desktop taking input is something other than the
// user's own: the lock screen, a UAC prompt or a screen saver.
func sessionLocked() bool {
	const desktopReadObjects, desktopSwitchDesktop, uoiName = 0x0001, 0x0100, 2
	h, _, _ := procOpenInputDesktop.Call(0, 0, desktopReadObjects|desktopSwitchDesktop)
	if h == 0 {
		return true
	}
	defer procCloseDesktop.Call(h)
	var name [64]uint16
	var need uint32
	r, _, _ := procGetUserObjectInfo.Call(h, uoiName, uintptr(unsafe.Pointer(&name[0])), unsafe.Sizeof(name), uintptr(unsafe.Pointer(&need)))
	return r == 0 || windows.UTF16ToString(name[:]) != "Default"
}

func lastInputTick() uint32 {
	var li struct{ size, time uint32 }
	li.size = uint32(unsafe.Sizeof(li))
	procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&li)))
	return li.time
}

// ---------- double Ctrl ----------

var (
	hookMu     sync.Mutex
	hookThread uint32
	hookProc   = syscall.NewCallback(keyboardProc)
)

// setCtrlHook installs or removes the keyboard hook. It's only installed while the
// setting is on.
func setCtrlHook(on bool) {
	hookMu.Lock()
	defer hookMu.Unlock()
	if on == (hookThread != 0) {
		return
	}
	if !on {
		const wmQuit = 0x0012
		procPostThreadMessage.Call(uintptr(hookThread), wmQuit, 0, 0)
		hookThread = 0
		return
	}
	ready := make(chan uint32)
	go func() {
		// A low-level hook belongs to the thread that installs it, and that thread
		// must keep reading messages.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		var msg struct {
			hwnd    uintptr
			message uint32
			wParam  uintptr
			lParam  uintptr
			time    uint32
			x, y    int32
			private uint32
		}
		procPeekMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0, 0) // gives the thread a message queue
		const whKeyboardLL = 13
		h, _, err := procSetWindowsHookEx.Call(whKeyboardLL, hookProc, 0, 0)
		if h == 0 {
			log.Printf("double Ctrl: couldn't install the keyboard hook: %v", err)
			ready <- 0
			return
		}
		ready <- windows.GetCurrentThreadId()
		for {
			if r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0); int32(r) <= 0 {
				break
			}
		}
		procUnhookWindowsHookEx.Call(h)
	}()
	hookThread = <-ready
}

// keyboardProc sees every key press, so it does as little as possible: it checks
// whether the key is Ctrl and keeps two timestamps. No key is stored or logged.
func keyboardProc(code, wParam, lParam uintptr) uintptr {
	if int32(code) == 0 {
		k := (*struct{ vk, scan, flags, time uint32 })(*(*unsafe.Pointer)(unsafe.Pointer(&lParam)))
		if ctrlTap(uint32(wParam), k.vk, k.flags, k.time) {
			requestWake("double Ctrl")
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, code, wParam, lParam)
	return r
}
