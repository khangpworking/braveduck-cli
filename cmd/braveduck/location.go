package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/geo"
	"github.com/khangpworking/braveduck-cli/internal/service"
)

func cmdLocation(ctx context.Context, svc *service.Service, args []string) error {
	if len(args) == 0 {
		l, err := svc.GetLocation(ctx)
		if err != nil {
			return err
		}
		if !l.Set {
			fmt.Println(`not set — braveduck location set "<your address>" (or set it on the board: 📍 Location)`)
			return nil
		}
		fmt.Printf("%s (%.5f, %.5f) — max commute %.0f km\n", orNone(l.Label), l.Lat, l.Lng, l.MaxKM)
		return nil
	}
	switch args[0] {
	case "clear":
		if err := svc.ClearLocation(ctx); err != nil {
			return err
		}
		fmt.Println("location and job distances cleared")
		return nil
	case "set":
	default:
		return errors.New(`usage: braveduck location [set "<address>" [--max-km N] | set --lat X --lng Y | clear]`)
	}
	rest := args[1:]
	var address string
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		address, rest = rest[0], rest[1:]
	}
	fs := flag.NewFlagSet("location set", flag.ContinueOnError)
	lat := fs.Float64("lat", 0, "latitude")
	lng := fs.Float64("lng", 0, "longitude")
	maxKM := fs.Float64("max-km", 0, "maximum commute in km (default: keep the current one, or 10)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	p, label := geo.Point{Lat: *lat, Lng: *lng}, ""
	if address != "" {
		places, err := svc.SearchPlaces(ctx, address)
		if err != nil {
			return err
		}
		if len(places) == 0 {
			return fmt.Errorf("no place matches %q — try adding the city", address)
		}
		p, label = places[0].Point, places[0].Label
	}
	l, err := svc.SetLocation(ctx, p, label, *maxKM)
	if err != nil {
		return err
	}
	fmt.Printf("location: %s (%.5f, %.5f), max commute %.0f km\n", orNone(l.Label), l.Lat, l.Lng, l.MaxKM)
	res, err := svc.UpdateDistances(ctx, service.MaxLookupsPerPass)
	if err != nil {
		return err
	}
	fmt.Printf("distances: %d job(s) measured", res.Measured)
	if res.Pending > 0 {
		fmt.Printf(", %d address(es) left for the board to look up over time", res.Pending)
	}
	fmt.Println()
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "(no name)"
	}
	return s
}
