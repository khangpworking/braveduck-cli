//go:build browser

package api

import (
	"testing"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/input"
)

func navWidth(page *rod.Page) float64 {
	return page.MustEval(`() => document.querySelector("nav").getBoundingClientRect().width`).Num()
}

// The menu folds into a strip of icons and back, by its button or "[", and
// stays folded after a reload; the icons still switch stages.
func TestAppMenuFolds(t *testing.T) {
	_, _, url := appBoard(t, appJobs())
	page := appPage(t, url, 1280)
	if w := navWidth(page); w < 200 {
		t.Fatalf("the menu starts folded (%v px)", w)
	}
	page.MustElement(`button[aria-label="Fold the menu away"]`).MustEval(`() => this.click()`)
	page.MustElement(`nav button[aria-label="Open the menu"]`)
	if w := navWidth(page); w > 80 {
		t.Errorf("the folded menu is %v px wide", w)
	}

	page.MustReload().MustWaitLoad()
	page.MustElement(`nav button[aria-label="Open the menu"]`)
	page.MustElement(`nav button[aria-label^="Applied"]`).MustEval(`() => this.click()`)
	page.MustElementR("h2", "^Applied")

	page.MustElement("body").MustEval(`() => document.activeElement.blur()`)
	page.Keyboard.MustType(input.BracketLeft)
	page.MustElement(`button[aria-label="Fold the menu away"]`)
}

// The bar between the list and the job drags to make one wider; the width
// stays after a reload, and a double-click evens them out again.
func TestAppListAndJobResize(t *testing.T) {
	_, _, url := appBoard(t, appJobs())
	page := appPage(t, url, 1280)
	listWidth := func() float64 {
		return page.MustEval(`() => document.querySelector("[data-panel][id=list], #list").getBoundingClientRect().width`).Num()
	}
	before := listWidth()
	bar := page.MustElement(`[data-slot=resizable-handle]`)
	box := bar.MustShape().Box()
	x, y := box.X+box.Width/2, box.Y+box.Height/2
	page.Mouse.MustMoveTo(x, y)
	page.Mouse.MustDown("left")
	page.Mouse.MustMoveTo(x+150, y)
	page.Mouse.MustUp("left")
	after := listWidth()
	if after < before+100 {
		t.Fatalf("dragging the bar 150 px: the list went from %v to %v px", before, after)
	}

	page.MustReload().MustWaitLoad()
	page.MustElement("[data-job]")
	if w := listWidth(); w < after-5 || w > after+5 {
		t.Errorf("after a reload the list is %v px, want about %v", w, after)
	}

	page.MustElement(`[data-slot=resizable-handle]`).MustEval(`() => this.dispatchEvent(new MouseEvent("dblclick", {bubbles: true}))`)
	page.MustWait(`w => Math.abs(document.querySelector("[data-panel][id=list], #list").getBoundingClientRect().width - w) < 5`, before)
}
