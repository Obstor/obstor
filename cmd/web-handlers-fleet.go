package cmd

import (
	"net/http"
	"sort"
	"time"

	"github.com/obstor/obstor/pkg/env"
)

// Fleet-summed usage card payload
type FleetStorageInfoRep struct {
	Used         uint64 `json:"used"`
	Total        uint64 `json:"total"`
	Free         uint64 `json:"free"`
	ObjectsCount uint64 `json:"objectsCount"`
	BucketsCount uint64 `json:"bucketsCount"`
	DisksOnline  int    `json:"disksOnline"`
	DisksOffline int    `json:"disksOffline"`
	UIVersion    string `json:"uiVersion"`
}

// Each member in the servers grid
type FleetServerRow struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	Region       string `json:"region"`
	Online       bool   `json:"online"`
	DrivesOnline int    `json:"drivesOnline"`
	DrivesTotal  int    `json:"drivesTotal"`
	Used         uint64 `json:"used"`
	Total        uint64 `json:"total"`
}

// Paginated member list payload
type FleetServersRep struct {
	Servers   []FleetServerRow `json:"servers"`
	Total     int              `json:"total"`
	UIVersion string           `json:"uiVersion"`
}

// A region's storage stats
type RegionStat struct {
	Region string `json:"region"`
	Used   uint64 `json:"used"`
	Total  uint64 `json:"total"`
}

// Regions breakdown payload
type FleetRegionsRep struct {
	Regions   []RegionStat `json:"regions"`
	TotalUsed uint64       `json:"totalUsed"`
	UIVersion string       `json:"uiVersion"`
}

// Daily bandwidth/ops payload
type FleetMetricsRep struct {
	Bandwidth   []BWBucket  `json:"bandwidth"`
	Ops         []OpsBucket `json:"ops"`
	WindowHours int         `json:"windowHours"`
	UIVersion   string      `json:"uiVersion"`
}

// New enroll token and the join command
type FleetEnrollTokenRep struct {
	Token     string `json:"token"`
	Command   string `json:"command"`
	UIVersion string `json:"uiVersion"`
}

// Calculate online members into the usage payload
func aggStorage(views []MemberView) FleetStorageInfoRep {
	var r FleetStorageInfoRep
	for _, v := range views {
		if !v.Online {
			continue
		}
		r.Used += v.Used
		r.Total += v.Total
		r.Free += v.Free
		r.ObjectsCount += v.ObjectsCount
		r.BucketsCount += v.BucketsCount
		r.DisksOnline += v.DrivesOnline
		r.DisksOffline += v.DrivesTotal - v.DrivesOnline
	}
	return r
}

// Online members by region with aggregate used and total bytes.
func regionStats(views []MemberView) []RegionStat {
	byRegion := map[string]*RegionStat{}
	for _, v := range views {
		if !v.Online || v.Region == "" {
			continue
		}
		rs := byRegion[v.Region]
		if rs == nil {
			rs = &RegionStat{Region: v.Region}
			byRegion[v.Region] = rs
		}
		rs.Used += v.Used
		rs.Total += v.Total
	}
	out := make([]RegionStat, 0, len(byRegion))
	for _, rs := range byRegion {
		out = append(out, *rs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Region < out[j].Region })
	return out
}

// Sort all members by ID and return page total
func fleetServersPage(views []MemberView, offset, limit int) ([]FleetServerRow, int) {
	sort.Slice(views, func(i, j int) bool { return views[i].ID < views[j].ID })
	total := len(views)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	if limit <= 0 {
		limit = 256
	}
	end := offset + limit
	if end > total {
		end = total
	}
	rows := make([]FleetServerRow, 0, end-offset)
	for _, v := range views[offset:end] {
		rows = append(rows, FleetServerRow{
			ID: v.ID, Label: v.Label, Region: v.Region, Online: v.Online,
			DrivesOnline: v.DrivesOnline, DrivesTotal: v.DrivesTotal,
			Used: v.Used, Total: v.Total,
		})
	}
	return rows, total
}

// Only a caller allowed to list every bucket may read them
func fleetGuard(r *http.Request, args *WebGenericArgs, op string) (FleetStore, error) {
	ctx := newWebContext(r, args, op)
	if globalFleetStore == nil {
		return nil, toJSONError(ctx, errServerNotInitialized)
	}
	claims, owner, authErr := webRequestAuthenticate(r)
	if authErr != nil {
		return nil, toJSONError(ctx, authErr)
	}
	if !callerCanSeeInstanceTotals(ctx, r, claims.AccessKey, claims.Map(), owner) {
		return nil, toJSONError(ctx, errAccessDenied)
	}
	return globalFleetStore, nil
}

// Fleet-summed storage usage
func (web *webAPIHandlers) FleetStorageInfo(r *http.Request, args *WebGenericArgs, reply *FleetStorageInfoRep) error {
	store, err := fleetGuard(r, args, "WebFleetStorageInfo")
	if err != nil {
		return err
	}
	*reply = aggStorage(store.Members(time.Now()))
	reply.UIVersion = Version
	return nil
}

// Paginated list of fleet members
func (web *webAPIHandlers) FleetServers(r *http.Request, args *ListServersArgs, reply *FleetServersRep) error {
	gen := &WebGenericArgs{}
	store, err := fleetGuard(r, gen, "WebFleetServers")
	if err != nil {
		return err
	}
	rows, total := fleetServersPage(store.Members(time.Now()), args.Offset, args.Limit)
	reply.Servers = rows
	reply.Total = total
	reply.UIVersion = Version
	return nil
}

// Per-region utilization stats for online members
func (web *webAPIHandlers) FleetRegions(r *http.Request, args *WebGenericArgs, reply *FleetRegionsRep) error {
	store, err := fleetGuard(r, args, "WebFleetRegions")
	if err != nil {
		return err
	}
	regions := regionStats(store.Members(time.Now()))
	reply.Regions = regions
	for _, rs := range regions {
		reply.TotalUsed += rs.Used
	}
	reply.UIVersion = Version
	return nil
}

// Daily bandwidth and ops ring series.
func (web *webAPIHandlers) FleetMetrics(r *http.Request, args *WebGenericArgs, reply *FleetMetricsRep) error {
	store, err := fleetGuard(r, args, "WebFleetMetrics")
	if err != nil {
		return err
	}
	bw, ops := store.Series()
	reply.Bandwidth = bw
	reply.Ops = ops
	reply.WindowHours = fleetRingSize
	reply.UIVersion = Version
	return nil
}

// Short-lived enroll token and returns the join command
func (web *webAPIHandlers) FleetEnrollToken(r *http.Request, args *WebGenericArgs, reply *FleetEnrollTokenRep) error {
	ctx := newWebContext(r, args, "WebFleetEnrollToken")
	_, owner, err := webRequestAuthenticate(r)
	if err != nil {
		return toJSONError(ctx, err)
	}
	if !owner {
		return toJSONError(ctx, errAccessDenied)
	}
	if globalFleetStore == nil {
		return toJSONError(ctx, errServerNotInitialized)
	}
	store := globalFleetStore
	tok := store.MintEnrollToken(time.Now())
	reply.Token = tok
	reply.Command = "obstor server <DATA_DIRS> --fleet-controller-url " + fleetControllerURL(r) + " --fleet-token " + tok
	reply.UIVersion = Version
	return nil
}

// Derives the URL members should report to
func fleetControllerURL(r *http.Request) string {
	if u := env.Get("OBSTOR_FLEET_CONTROLLER_URL", ""); u != "" {
		return u
	}
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}
