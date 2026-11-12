// mkrsrc writes rsrc_windows_amd64.syso so nodoff.exe carries its icon, version
// information and an application manifest. Run from the repo root: go run ./tools/mkrsrc
package main

import (
	"image"
	"log"
	"os"
	"regexp"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"

	"github.com/Tombo1001/nodoff/internal/logo"
)

func main() {
	var imgs []image.Image
	for _, n := range []int{16, 20, 24, 32, 40, 48, 64, 128, 256} {
		imgs = append(imgs, logo.Render(n, logo.Amber))
	}
	icon, err := winres.NewIconFromImages(imgs)
	if err != nil {
		log.Fatal(err)
	}
	rs := winres.ResourceSet{}
	if err := rs.SetIcon(winres.Name("APPICON"), icon); err != nil {
		log.Fatal(err)
	}
	src, err := os.ReadFile("version.go")
	if err != nil {
		log.Fatal(err)
	}
	m := regexp.MustCompile(`const version = "([0-9.]+)"`).FindSubmatch(src)
	if m == nil {
		log.Fatal("version.go: no version constant")
	}
	v := version.Info{}
	v.SetFileVersion(string(m[1]))
	v.SetProductVersion(string(m[1]))
	v.Set(version.LangDefault, version.ProductName, "nodoff")
	v.Set(version.LangDefault, version.FileDescription, "nodoff: monitor brightness and power over MQTT")
	v.Set(version.LangDefault, version.OriginalFilename, "nodoff.exe")
	rs.SetVersionInfo(v)
	// asInvoker: never ask for admin rights. Per-monitor DPI awareness, and Windows 10/11 behaviour.
	rs.SetManifest(winres.AppManifest{
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		Compatibility:       winres.Win10AndAbove,
		UseCommonControlsV6: true,
	})
	f, err := os.Create("rsrc_windows_amd64.syso")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := rs.WriteObject(f, winres.ArchAMD64); err != nil {
		log.Fatal(err)
	}
}
