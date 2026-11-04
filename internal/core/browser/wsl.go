package browser

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// On WSL, a Linux window is shown on the Windows desktop by WSLg — but
// Windows won't let a background program take focus, so the login window
// opens *behind* whatever the user is looking at and they never see it. So
// braveduck asks Windows (powershell.exe via WSL interop) to restore the
// window if minimized and focus it. A synthetic Alt key press first lifts
// Windows' foreground lock for that one call — the standard workaround.
// (Minimize-then-restore is NOT used: on WSLg the minimize sticks and the
// restore can be lost, leaving the window parked in the taskbar.)

// wslDistro returns the WSL distro name, or "" when not running under WSL.
func wslDistro() string {
	if d := os.Getenv("WSL_DISTRO_NAME"); d != "" {
		return d
	}
	if b, err := os.ReadFile("/proc/version"); err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft") {
		return "WSL"
	}
	return ""
}

// raiseScript brings to the front the first Windows window whose title
// contains every one of parts. Each part is embedded as a PowerShell
// single-quoted literal (quotes doubled), so a page title can't break out.
func raiseScript(parts []string) string {
	var quoted []string
	for _, p := range parts {
		quoted = append(quoted, "'"+strings.ReplaceAll(p, "'", "''")+"'")
	}
	return `Add-Type @"
using System; using System.Runtime.InteropServices; using System.Text;
public class BdRaise {
  delegate bool EnumProc(IntPtr h, IntPtr l);
  [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc f, IntPtr l);
  [DllImport("user32.dll")] static extern int GetWindowText(IntPtr h, StringBuilder s, int n);
  [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll")] static extern bool IsIconic(IntPtr h);
  [DllImport("user32.dll")] static extern bool ShowWindow(IntPtr h, int c);
  [DllImport("user32.dll")] static extern bool BringWindowToTop(IntPtr h);
  [DllImport("user32.dll")] static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] static extern void keybd_event(byte vk, byte scan, uint flags, UIntPtr extra);
  public static bool Raise(string[] parts) {
    IntPtr found = IntPtr.Zero;
    EnumWindows((h, l) => {
      if (!IsWindowVisible(h)) return true;
      var sb = new StringBuilder(512); GetWindowText(h, sb, 512); var t = sb.ToString();
      foreach (var p in parts) if (!t.Contains(p)) return true;
      found = h; return false; }, IntPtr.Zero);
    if (found == IntPtr.Zero) return false;
    if (IsIconic(found)) ShowWindow(found, 9);    // SW_RESTORE
    keybd_event(0x12, 0, 0, UIntPtr.Zero);        // Alt down
    keybd_event(0x12, 0, 2, UIntPtr.Zero);        // Alt up
    BringWindowToTop(found);
    return SetForegroundWindow(found);
  }
}
"@
if ([BdRaise]::Raise(@(` + strings.Join(quoted, ",") + `))) { "raised" } else { "missing" }`
}

// raiseOnWSL tries for up to ~20s (the window appears a moment after
// launch) to bring the Chromium window for this distro to the front. Best
// effort: it does nothing outside WSL or without powershell.exe.
func raiseOnWSL(ctx context.Context) {
	distro := wslDistro()
	if distro == "" {
		return
	}
	ps, err := exec.LookPath("powershell.exe")
	if err != nil {
		return
	}
	// WSLg titles Linux windows "<page title> - Chromium (<distro>)".
	parts := []string{"Chromium", "(" + distro + ")"}
	if distro == "WSL" {
		parts = []string{"Chromium ("}
	}
	script := raiseScript(parts)
	for i := 0; i < 10; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		cmd := exec.CommandContext(cctx, ps, "-NoProfile", "-NonInteractive", "-Command", script)
		cmd.Dir = "/" // powershell warns about UNC working dirs otherwise
		out, err := cmd.Output()
		cancel()
		if err == nil && strings.Contains(string(out), "raised") {
			return
		}
	}
}
