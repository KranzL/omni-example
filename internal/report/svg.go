package report

import (
	"fmt"
	"html"
	"math"
	"sort"
	"strings"

	"github.com/KranzL/omni-example/internal/llm"
)

const svgStyle = `<style>
svg { --surface: #fcfcfb; --ink: #0b0b0b; --ink-2: #52514e; --grid: #e4e3df; --axis: #9a9893; --s1: #2a78d6; --s2: #eb6834; --front: #52514e; }
@media (prefers-color-scheme: dark) {
svg { --surface: #1a1a19; --ink: #ffffff; --ink-2: #c3c2b7; --grid: #33332f; --axis: #6f6e69; --s1: #3987e5; --s2: #d95926; --front: #c3c2b7; }
}
.bg { fill: var(--surface); }
.t { fill: var(--ink); font-family: -apple-system, "Segoe UI", Helvetica, Arial, sans-serif; font-size: 11px; }
.t2 { fill: var(--ink-2); font-family: -apple-system, "Segoe UI", Helvetica, Arial, sans-serif; font-size: 11px; }
.h { fill: var(--ink); font-family: -apple-system, "Segoe UI", Helvetica, Arial, sans-serif; font-size: 14px; font-weight: 600; }
.grid { stroke: var(--grid); stroke-width: 1; }
.axis { stroke: var(--axis); stroke-width: 1; }
.lead { stroke: var(--axis); stroke-width: 1; }
.front { stroke: var(--front); stroke-width: 1.5; stroke-dasharray: 5 4; fill: none; }
.p1 { fill: var(--s1); stroke: var(--surface); stroke-width: 2; }
.p2 { fill: var(--s2); stroke: var(--surface); stroke-width: 2; }
.bar { fill: var(--s1); }
.bar2 { fill: var(--s2); }
.halo { paint-order: stroke; stroke: var(--surface); stroke-width: 3px; stroke-linejoin: round; }
</style>
`

type box struct{ x0, y0, x1, y1 float64 }

func (b box) overlaps(o box) bool {
	return b.x0 < o.x1 && o.x0 < b.x1 && b.y0 < o.y1 && o.y0 < b.y1
}

func textWidth(s string) float64 {
	return 6.1 * float64(len(s))
}

func esc(s string) string {
	return html.EscapeString(s)
}

func providerClass(p string) string {
	if p == llm.ProviderAnthropic {
		return "p1"
	}
	return "p2"
}

func shortLabel(c Config) string {
	name := strings.TrimPrefix(c.Label, c.Provider+"/")
	if c.Provider == llm.ProviderAnthropic {
		return name
	}
	return c.Provider + "/" + name
}

func niceLogTicks(lo, hi float64) []float64 {
	var out []float64
	for e := math.Floor(math.Log10(lo)); e <= math.Ceil(math.Log10(hi)); e++ {
		for _, m := range []float64{1, 2, 5} {
			v := m * math.Pow(10, e)
			if v >= lo && v <= hi {
				out = append(out, v)
			}
		}
	}
	return out
}

func tickLabel(v float64) string {
	switch {
	case v >= 1:
		return fmt.Sprintf("$%.0f", v)
	case v >= 0.1:
		return fmt.Sprintf("$%.1f", v)
	case v >= 0.01:
		return fmt.Sprintf("$%.2f", v)
	default:
		return fmt.Sprintf("$%.3f", v)
	}
}

func ParetoSVG(cs []Config) string {
	const (
		w, h                   = 900.0, 560.0
		left, right, top, bott = 70.0, 190.0, 56.0, 60.0
	)
	pw, ph := w-left-right, h-top-bott
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.0f %.0f" width="%.0f" height="%.0f" role="img" aria-label="Cost per pass versus accuracy, one point per configuration">`+"\n", w, h, w, h)
	sb.WriteString(svgStyle)
	fmt.Fprintf(&sb, `<rect class="bg" x="0" y="0" width="%.0f" height="%.0f"/>`+"\n", w, h)
	n := 0
	if len(cs) > 0 {
		n = cs[0].Questions()
	}
	fmt.Fprintf(&sb, `<text class="h" x="%.0f" y="24">Cost per pass over %d questions versus accuracy, one point per configuration</text>`+"\n", left, n)
	if len(cs) == 0 {
		sb.WriteString("</svg>\n")
		return sb.String()
	}
	lo, hi := math.Inf(1), 0.0
	minAcc := 1.0
	for _, c := range cs {
		if v := c.PassCostUSD(); v > 0 {
			lo = math.Min(lo, v)
			hi = math.Max(hi, v)
		}
		minAcc = math.Min(minAcc, c.Summary.Accuracy)
	}
	if math.IsInf(lo, 1) {
		lo, hi = 0.001, 0.01
	}
	lo, hi = lo/1.3, hi*1.3
	yLo := math.Floor(minAcc*10-0.5) / 10
	if yLo < 0 {
		yLo = 0
	}
	xOf := func(v float64) float64 {
		if v < lo {
			v = lo
		}
		return left + pw*(math.Log10(v)-math.Log10(lo))/(math.Log10(hi)-math.Log10(lo))
	}
	yOf := func(a float64) float64 {
		return top + ph*(1-(a-yLo)/(1-yLo))
	}
	for a := yLo; a <= 1.0001; a += 0.1 {
		y := yOf(a)
		fmt.Fprintf(&sb, `<line class="grid" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`+"\n", left, y, left+pw, y)
		fmt.Fprintf(&sb, `<text class="t2" x="%.1f" y="%.1f" text-anchor="end">%.1f</text>`+"\n", left-8, y+4, a)
	}
	for _, v := range niceLogTicks(lo, hi) {
		x := xOf(v)
		fmt.Fprintf(&sb, `<line class="grid" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`+"\n", x, top, x, top+ph)
		fmt.Fprintf(&sb, `<text class="t2" x="%.1f" y="%.1f" text-anchor="middle">%s</text>`+"\n", x, top+ph+18, tickLabel(v))
	}
	fmt.Fprintf(&sb, `<line class="axis" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`+"\n", left, top+ph, left+pw, top+ph)
	fmt.Fprintf(&sb, `<text class="t" x="%.1f" y="%.1f" text-anchor="middle">Total cost of the run in USD (log scale)</text>`+"\n", left+pw/2, h-16)
	fmt.Fprintf(&sb, `<text class="t" transform="translate(18 %.1f) rotate(-90)" text-anchor="middle">Accuracy (lenient)</text>`+"\n", top+ph/2)

	front := Frontier(cs)
	for _, prov := range providers(cs) {
		var fx, fy []float64
		for _, c := range sortByCost(byProvider(cs, prov)) {
			if front[c.Label] {
				fx = append(fx, xOf(c.PassCostUSD()))
				fy = append(fy, yOf(c.Summary.Accuracy))
			}
		}
		if len(fx) > 1 {
			var pts []string
			for i := range fx {
				if i > 0 {
					pts = append(pts, fmt.Sprintf("%.1f,%.1f", fx[i], fy[i-1]))
				}
				pts = append(pts, fmt.Sprintf("%.1f,%.1f", fx[i], fy[i]))
			}
			fmt.Fprintf(&sb, `<polyline class="front" points="%s"/>`+"\n", strings.Join(pts, " "))
		}
	}

	type pt struct {
		c    Config
		x, y float64
	}
	var pts []pt
	for _, c := range cs {
		pts = append(pts, pt{c, xOf(c.PassCostUSD()), yOf(c.Summary.Accuracy)})
	}
	sort.SliceStable(pts, func(i, j int) bool {
		if pts[i].y != pts[j].y {
			return pts[i].y < pts[j].y
		}
		return pts[i].x < pts[j].x
	})
	var taken []box
	for _, p := range pts {
		taken = append(taken, box{p.x - 7, p.y - 7, p.x + 7, p.y + 7})
	}
	var marks, labels strings.Builder
	for _, p := range pts {
		cls := providerClass(p.c.Provider)
		title := fmt.Sprintf("%s: %d/%d correct, total $%.4f, $%.4f per pass, $%.5f per correct", p.c.Label, p.c.Summary.Correct, p.c.Summary.Total, p.c.CostUSD, p.c.PassCostUSD(), p.c.CostPerCorrect())
		if p.c.Baseline {
			fmt.Fprintf(&marks, `<rect class="%s" x="%.1f" y="%.1f" width="11" height="11" rx="1.5"><title>%s</title></rect>`+"\n", cls, p.x-5.5, p.y-5.5, esc(title))
		} else {
			fmt.Fprintf(&marks, `<circle class="%s" cx="%.1f" cy="%.1f" r="6"><title>%s</title></circle>`+"\n", cls, p.x, p.y, esc(title))
		}
		text := shortLabel(p.c)
		tw := textWidth(text)
		placed := false
		for _, dy := range []float64{0, -13, 13, -26, 26, -39, 39, -52, 52, -65, 65} {
			for _, side := range []float64{1, -1} {
				var b box
				ly := p.y + dy
				if side > 0 {
					b = box{p.x + 9, ly - 9, p.x + 9 + tw, ly + 3}
				} else {
					b = box{p.x - 9 - tw, ly - 9, p.x - 9, ly + 3}
				}
				if b.x0 < left+2 || b.x1 > w-4 || b.y0 < top-12 || b.y1 > top+ph-2 {
					continue
				}
				clash := false
				for _, o := range taken {
					if b.overlaps(o) {
						clash = true
						break
					}
				}
				if clash {
					continue
				}
				taken = append(taken, b)
				anchor := "start"
				tx := b.x0
				if side < 0 {
					anchor = "end"
					tx = b.x1
				}
				if dy != 0 {
					fmt.Fprintf(&labels, `<line class="lead" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`+"\n", p.x, p.y, tx-side*2, ly-3)
				}
				fmt.Fprintf(&labels, `<text class="t halo" x="%.1f" y="%.1f" text-anchor="%s">%s</text>`+"\n", tx, ly, anchor, esc(text))
				placed = true
				break
			}
			if placed {
				break
			}
		}
	}
	sb.WriteString(labels.String())
	sb.WriteString(marks.String())

	lx, ly := left+pw+24, top+8.0
	fmt.Fprintf(&sb, `<text class="t" x="%.1f" y="%.1f" font-weight="600">Legend</text>`+"\n", lx, ly)
	items := []struct{ cls, shape, text string }{
		{"p1", "rect", "Anthropic baseline"},
		{"p1", "circle", "Anthropic router"},
		{"p2", "rect", "Venice baseline"},
		{"p2", "circle", "Venice router"},
	}
	for i, it := range items {
		y := ly + 22 + float64(i)*20
		if it.shape == "rect" {
			fmt.Fprintf(&sb, `<rect class="%s" x="%.1f" y="%.1f" width="11" height="11" rx="1.5"/>`+"\n", it.cls, lx, y-9)
		} else {
			fmt.Fprintf(&sb, `<circle class="%s" cx="%.1f" cy="%.1f" r="6"/>`+"\n", it.cls, lx+5.5, y-3.5)
		}
		fmt.Fprintf(&sb, `<text class="t" x="%.1f" y="%.1f">%s</text>`+"\n", lx+18, y, it.text)
	}
	y := ly + 22 + float64(len(items))*20
	fmt.Fprintf(&sb, `<line class="front" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`+"\n", lx, y-4, lx+12, y-4)
	fmt.Fprintf(&sb, `<text class="t" x="%.1f" y="%.1f">Pareto frontier per provider</text>`+"\n", lx+18, y)
	fmt.Fprintf(&sb, `<text class="t2" x="%.1f" y="%.1f">Square: always one tier.</text>`+"\n", lx, y+28)
	fmt.Fprintf(&sb, `<text class="t2" x="%.1f" y="%.1f">Circle: routed or cascaded.</text>`+"\n", lx, y+44)
	fmt.Fprintf(&sb, `<text class="t2" x="%.1f" y="%.1f">Up and left is better.</text>`+"\n", lx, y+60)
	sb.WriteString("</svg>\n")
	return sb.String()
}

func DifficultySVG(cs []Config) string {
	const (
		cols           = 4
		pw, ph         = 210.0, 150.0
		gap            = 16.0
		marginX, headH = 20.0, 56.0
		barW           = 30.0
	)
	rows := (len(cs) + cols - 1) / cols
	w := marginX*2 + cols*pw + (cols-1)*gap
	h := headH + float64(rows)*(ph+gap) + 10
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.0f %.0f" width="%.0f" height="%.0f" role="img" aria-label="Accuracy by difficulty, one panel per configuration">`+"\n", w, h, w, h)
	sb.WriteString(svgStyle)
	fmt.Fprintf(&sb, `<rect class="bg" x="0" y="0" width="%.0f" height="%.0f"/>`+"\n", w, h)
	fmt.Fprintf(&sb, `<text class="h" x="%.0f" y="24">Accuracy by difficulty, lenient verdict, one panel per configuration</text>`+"\n", marginX)
	fmt.Fprintf(&sb, `<text class="t2" x="%.0f" y="42">Bar height is the share correct; the label is correct over asked, counted once per pass. E easy, M moderate, H hard, X expert. Blue Anthropic, orange Venice.</text>`+"\n", marginX)
	short := map[string]string{"easy": "E", "moderate": "M", "hard": "H", "expert": "X"}
	for i, c := range cs {
		ox := marginX + float64(i%cols)*(pw+gap)
		oy := headH + float64(i/cols)*(ph+gap)
		plotTop, plotBot := oy+50, oy+ph-20
		fmt.Fprintf(&sb, `<text class="t" x="%.1f" y="%.1f" font-weight="600">%s</text>`+"\n", ox, oy+14, esc(shortLabel(c)))
		fmt.Fprintf(&sb, `<text class="t2" x="%.1f" y="%.1f">%d/%d, %s, $%.4f</text>`+"\n", ox, oy+28, c.Summary.Correct, c.Summary.Total, passesText(c.Passes()), c.CostUSD)
		fmt.Fprintf(&sb, `<line class="grid" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`+"\n", ox, plotTop, ox+pw-10, plotTop)
		fmt.Fprintf(&sb, `<line class="axis" x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f"/>`+"\n", ox, plotBot, ox+pw-10, plotBot)
		slot := (pw - 10) / float64(len(Difficulties))
		for j, d := range Difficulties {
			cx := ox + slot*float64(j) + slot/2
			fmt.Fprintf(&sb, `<text class="t2" x="%.1f" y="%.1f" text-anchor="middle">%s</text>`+"\n", cx, plotBot+14, short[d])
			ds, ok := c.Summary.ByDifficulty[d]
			if !ok || ds.Total == 0 {
				fmt.Fprintf(&sb, `<text class="t2" x="%.1f" y="%.1f" text-anchor="middle">not run</text>`+"\n", cx, plotBot-4)
				continue
			}
			bh := (plotBot - plotTop) * ds.Accuracy
			if bh > 0 {
				r := math.Min(3, bh/2)
				x0, x1, y0 := cx-barW/2, cx+barW/2, plotBot-bh
				fmt.Fprintf(&sb, `<path class="%s" d="M%.1f %.1f V%.1f Q%.1f %.1f %.1f %.1f H%.1f Q%.1f %.1f %.1f %.1f V%.1f Z"><title>%s %s: %d/%d over %s</title></path>`+"\n",
					barClass(c.Provider), x0, plotBot, y0+r, x0, y0, x0+r, y0, x1-r, x1, y0, x1, y0+r, plotBot, esc(c.Label), d, ds.Correct, ds.Total, passesText(c.Passes()))
			}
			fmt.Fprintf(&sb, `<text class="t" x="%.1f" y="%.1f" text-anchor="middle">%d/%d</text>`+"\n", cx, plotBot-bh-4, ds.Correct, ds.Total)
		}
	}
	sb.WriteString("</svg>\n")
	return sb.String()
}

func barClass(p string) string {
	if p == llm.ProviderAnthropic {
		return "bar"
	}
	return "bar2"
}
