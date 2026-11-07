package bench

import (
	"fmt"
	"strings"
)

// The fixed world every system sees. Names are fictional so a model can't answer from memory.

type laptop struct{ Name, URL, Weight, Battery, Price, Notes string }

var laptops = []laptop{
	{"Lumen Aero 13", "https://www.notebookreview.net/lumen-aero-13-review", "1.05 kg", "17 hours", "US$1,299", "13.3-inch OLED, 16 GB RAM, excellent build"},
	{"Halcyon Nimbus Air 14", "https://www.notebookreview.net/halcyon-nimbus-air-14-review", "1.20 kg", "14 hours", "US$1,449", "14-inch screen, best keyboard in its class"},
	{"Corvid Voyager X13", "https://www.notebookreview.net/corvid-voyager-x13-review", "0.98 kg", "12 hours", "US$1,199", "13.3-inch screen, lightest tested, average colour accuracy"},
	{"Ardent Titan Pro 16", "https://www.notebookreview.net/ardent-titan-pro-16-review", "2.10 kg", "9 hours", "US$1,899", "16-inch screen, very fast but heavy"},
	{"Kestrel Breeze 15", "https://www.notebookreview.net/kestrel-breeze-15-review", "1.65 kg", "10 hours", "US$999", "15.6-inch screen, budget pick, heavier"},
}

type place struct{ Name, Area, Tip string }

var tokyo = []place{
	{"Senso-ji", "Asakusa", "Tokyo's oldest temple; go early to beat crowds"},
	{"teamLab Planets", "Toyosu", "immersive digital art; book timed tickets"},
	{"Shibuya Crossing", "Shibuya", "busiest crossing; view from Shibuya Sky at sunset"},
	{"Meiji Jingu", "Harajuku", "forest shrine next to Takeshita Street"},
	{"Tsukiji Outer Market", "Tsukiji", "street food breakfast, closes early afternoon"},
	{"Shinjuku Gyoen", "Shinjuku", "large garden, great for a slow afternoon"},
	{"Akihabara", "Akihabara", "electronics, anime and retro games"},
	{"Tokyo Skytree", "Oshiage", "observation decks; pairs well with Asakusa"},
}

const sourdough = "Beginner sourdough loaf (one loaf): 500 g bread flour, 350 g water, 100 g active sourdough starter, 10 g salt. " +
	"Optional: rice flour for dusting. Equipment: digital scale, large bowl, Dutch oven, proofing basket or bowl with a tea towel. " +
	"Method: mix flour and water, rest 1 hour, add starter and salt, 4 sets of stretch and folds over 2 hours, bulk ferment 4-6 hours, " +
	"shape, proof overnight in the fridge, bake at 250 C covered 20 min then uncovered 25 min."

type article struct{ Title, URL, Text string }

var solar = []article{
	{"Helios Home 13 kWh battery launches", "https://www.energystoragenews.net/helios-home-13-launch", "Helios launched the Home 13, a 13 kWh home battery priced at USD 9,800 installed. Shipping starts in December."},
	{"PowerVault 10 price cut", "https://www.energystoragenews.net/powervault-10-price-cut", "PowerVault cut the price of its 10 kWh battery to USD 7,200 installed, citing cheaper LFP cells."},
	{"Home battery subsidies extended", "https://www.energystoragenews.net/home-battery-subsidies-2027", "Several governments extended home battery subsidies into 2027, with rebates of up to 30 percent."},
}

const usdAUD = 1.52

type book struct {
	Title, Author string
	Year          int
}

var shelf = []book{
	{"The Glass Orchard", "Mira Okafor", 1987}, {"Salt and Static", "Devin Harrow", 2003}, {"A Map of Quiet Moons", "Lena Varga", 1994},
	{"The Ninth Lantern", "Tomas Reyes", 1979}, {"Ferrous Hearts", "Ada Lindqvist", 2011}, {"Under the Copper Sky", "Yusuf Adeyemi", 1968},
	{"The Cartographer's Daughter", "Hana Morita", 1999}, {"Signal Fires", "Owen Achterberg", 2015}, {"The Last Tidewalker", "Priya Raman", 1983},
	{"Ashes of Meridian", "Caleb Stroud", 1972}, {"The Paper Republic", "Ines Laurent", 2007}, {"Hollow Crown Station", "Marcus Feld", 1991},
	{"Winter in Kesh", "Sofia Bianchi", 1976}, {"The Clockmaker's War", "Ravi Menon", 2019}, {"Driftlight", "Elif Kaya", 1988},
	{"The Sunken Choir", "Jonah Pell", 2001}, {"Ironwood Saga", "Greta Holm", 1965}, {"The Orbit of Small Things", "Naoko Sato", 2013},
	{"Beneath the Amber Line", "Diego Paredes", 1997}, {"Echoes of Varn", "Tilde Bergman", 1981},
}

type restaurant struct {
	Name, Area, Cuisine string
	Available           []string
}

var restaurants = []restaurant{
	{"Bella Cucina", "West Village", "Italian", []string{"7:00pm", "8:30pm"}},
	{"Trattoria Roma", "West Village", "Italian", []string{"6:30pm"}},
}

const photosExported = 4182

var events = map[string]string{
	"newsletter": `New email from Pasta Weekly newsletter: "10 autumn pasta recipes you need to try".`,
	"flight":     `New email from Delta Air Lines: "Schedule change for DL 402 on Friday 6 November: now departs JFK at 7:45pm (was 11:00pm). Please arrive 2 hours before departure."`,
	"package":    `New email from UPS: "Your package was delivered to the front door."`,
}

const pricingPy = `def apply_discount(price, percent):
    """Return the price after taking off ` + "`percent`" + ` percent."""
    return round(price * percent / 100, 2)


def total(items, percent=0):
    """Sum quantity * unit price for each (name, qty, unit_price) item, then apply the discount."""
    subtotal = sum(qty * unit for _, qty, unit in items)
    return apply_discount(subtotal, percent)
`

const testPy = `import unittest

from pricing import apply_discount, total


class PricingTest(unittest.TestCase):
    def test_discount(self):
        self.assertEqual(apply_discount(200, 15), 170.0)

    def test_no_discount(self):
        self.assertEqual(apply_discount(80, 0), 80.0)

    def test_total(self):
        self.assertEqual(total([("pen", 3, 2.5), ("book", 1, 12.0)], 10), 17.55)


if __name__ == "__main__":
    unittest.main()
`

type result struct{ Title, URL, Snippet string }

func search(query string) any {
	q := strings.ToLower(strings.ReplaceAll(query, "'", ""))
	for _, b := range shelf {
		if strings.Contains(q, strings.ToLower(strings.ReplaceAll(b.Title, "'", ""))) {
			return []result{{fmt.Sprintf("%s (%d)", b.Title, b.Year), "https://www.bookdatabase.net/" + slug(b.Title),
				fmt.Sprintf("%s is a novel by %s, first published in %d.", b.Title, b.Author, b.Year)}}
		}
	}
	// Sites the user has accounts with come up by name, as they would on a real search engine.
	for host, about := range map[string]string{"pulse.fit": "Pulse Fitness: classes in Lower Manhattan", "fernway.shop": "Fernway: travel gear and luggage",
		"streamly.tv": "Streamly: films and series", "cobalt.edu": "Cobalt University student portal", "wayfarer.travel": "Wayfarer: tours and experiences",
		"ledgerline.app": "Ledgerline: CRM for sales teams"} {
		if strings.Contains(q, strings.SplitN(host, ".", 2)[0]) {
			return []result{{about, "https://" + host, about + "."}}
		}
	}
	switch {
	case containsAny(q, "laptop", "ultrabook", "notebook"):
		var out []result
		for _, l := range laptops {
			out = append(out, result{l.Name + " review", l.URL, fmt.Sprintf("%s: weighs %s. Full specs in review.", l.Name, l.Weight)})
		}
		return out
	case containsAny(q, "tokyo", "japan", "itinerary"):
		var out []result
		for _, p := range tokyo {
			out = append(out, result{p.Name, "https://www.tokyotravelguide.net/" + slug(p.Name), fmt.Sprintf("%s (%s): %s", p.Name, p.Area, p.Tip)})
		}
		return out
	case containsAny(q, "sourdough", "bread", "starter"):
		return []result{{"Beginner sourdough loaf", "https://www.breadbakingguide.net/beginner-sourdough", "A simple first loaf with only four ingredients. Full recipe on page."}}
	case containsAny(q, "solar", "battery", "batteries", "energy storage"):
		var out []result
		for _, a := range solar {
			out = append(out, result{a.Title, a.URL, strings.SplitN(a.Text, ",", 2)[0] + "..."})
		}
		return out
	}
	return map[string]any{"results": []result{}, "note": "No closely matching results. Try different keywords."}
}

func page(url string) string {
	for _, l := range laptops {
		if url == l.URL {
			return fmt.Sprintf("%s review. Weight %s. Battery life %s in our standard video-playback test. Street price %s in the US as of October 2026. %s.", l.Name, l.Weight, l.Battery, l.Price, l.Notes)
		}
	}
	for _, a := range solar {
		if url == a.URL {
			return a.Text
		}
	}
	if strings.Contains(url, "sourdough") {
		return sourdough
	}
	for _, p := range tokyo {
		if strings.HasSuffix(url, slug(p.Name)) {
			return fmt.Sprintf("%s, in %s. %s. Open daily.", p.Name, p.Area, p.Tip)
		}
	}
	return "HTTP 403: this site blocks automated access."
}

func shelfPage(n int) map[string]any {
	if n < 1 {
		n = 1
	}
	start := (n - 1) * 5
	var titles []string
	for i := start; i < start+5 && i < len(shelf); i++ {
		titles = append(titles, shelf[i].Title)
	}
	var next any
	if start+5 < len(shelf) {
		next = n + 1
	}
	return map[string]any{"page": n, "books": titles, "next_page": next, "total_books": len(shelf)}
}

func slug(s string) string { return strings.ReplaceAll(strings.ToLower(s), " ", "-") }

func containsAny(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}
