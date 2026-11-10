package model

import (
	"os"
	"testing"
)

func TestEICARStringIsTheStandard68ByteString(t *testing.T) {
	// The EICAR anti-malware test file is a published, harmless 68-byte
	// printable-ASCII string used to verify that scanners are working.
	const want = `X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`
	if EICAR != want {
		t.Fatalf("EICAR = %q", EICAR)
	}
	if len(EICAR) != 68 {
		t.Errorf("EICAR is %d bytes, want 68", len(EICAR))
	}
	for i, c := range []byte(EICAR) {
		if c < 0x20 || c > 0x7e {
			t.Errorf("byte %d (%#x) is not printable ASCII", i, c)
		}
	}
}

func TestIsEICARName(t *testing.T) {
	cases := map[string]bool{
		"eicar":      true,
		"EICAR":      true,
		"Eicar":      true,
		"eIcAr":      true,
		"eicar.com":  true,
		"eicar.txt":  true,
		"EICAR.ZIP":  true,
		"sub/eicar":  true,
		"./eicar":    true,
		"  eicar  ":  true,
		"eicarx":     false,
		"myeicar":    false,
		"eicar2":     false,
		"eicar test": false,
		"report":     false,
		"":           false,
	}
	for name, want := range cases {
		if got := IsEICARName(name); got != want {
			t.Errorf("IsEICARName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestServiceCreateEICARWritesTheStringAtSmallestSize(t *testing.T) {
	svc, dir := newTestService(t)
	res, err := svc.Create(Request{Dir: dir, Name: "eicar", Ext: "txt"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.EICAR {
		t.Error("result must be flagged as an EICAR file")
	}
	if res.Size != 68 {
		t.Errorf("size = %d, want 68 (smallest possible)", res.Size)
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != EICAR {
		t.Errorf("content = %q", data)
	}
}

func TestServiceCreateEICARUsesChosenExtension(t *testing.T) {
	svc, dir := newTestService(t)
	// "jpg" has a minimum far above 68 bytes; the EICAR override must ignore it.
	res, err := svc.Create(Request{Dir: dir, Name: "EiCaR", Ext: ".JPEG"})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Path[len(dir)+1:]; got != "EiCaR.jpg" {
		t.Errorf("file name = %q, want EiCaR.jpg", got)
	}
	if res.Size != 68 {
		t.Errorf("size = %d, want 68", res.Size)
	}
}

func TestServiceCreateEICARIgnoresRequestedSize(t *testing.T) {
	svc, dir := newTestService(t)
	size := int64(100000)
	res, err := svc.Create(Request{Dir: dir, Name: "eicar", Ext: "txt", Size: &size})
	if err != nil {
		t.Fatal(err)
	}
	if res.Size != 68 {
		t.Errorf("an EICAR file is always 68 bytes, got %d", res.Size)
	}
}

func TestServiceCreateEICARStillRefusesOverwrite(t *testing.T) {
	svc, dir := newTestService(t)
	if _, err := svc.Create(Request{Dir: dir, Name: "eicar", Ext: "txt"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Create(Request{Dir: dir, Name: "eicar", Ext: "txt"}); err == nil {
		t.Error("a second create without Overwrite must fail")
	}
}

func TestServiceCreateNonEICARNameIsUnaffected(t *testing.T) {
	svc, dir := newTestService(t)
	res, err := svc.Create(Request{Dir: dir, Name: "report", Ext: "txt"})
	if err != nil {
		t.Fatal(err)
	}
	if res.EICAR {
		t.Error("an ordinary file must not be flagged as EICAR")
	}
	data, _ := os.ReadFile(res.Path)
	if string(data) == EICAR {
		t.Error("an ordinary file must use its format generator, not the EICAR string")
	}
}
