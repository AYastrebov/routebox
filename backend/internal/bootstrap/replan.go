package bootstrap

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"

	"routebox/backend/internal/util"
)

// Install is what CaddyParamsFromInstall needs from settings and the process:
// everything the Caddyfile depends on that the active sing-box config does not
// already say.
type Install struct {
	Domain, Dest, PanelPath, Caddyfile, StubRoot, ACMEEmail string
	FrontPort, PanelPort, WebFrontPort                      int
}

// ErrReplanSkipped means the install no longer has what a Caddyfile is planned
// from (an inbound was removed or reshaped by hand). It is not a failure: the
// Caddyfile on disk keeps working, so the caller leaves it alone.
var ErrReplanSkipped = errors.New("caddyfile re-plan skipped")

// ReplanDisabled is the operator's switch for hand-maintained Caddyfiles.
func ReplanDisabled() bool {
	switch strings.ToLower(os.Getenv("BOOTSTRAP_CADDYFILE_REPLAN")) {
	case "0", "false":
		return true
	}
	return false
}

// CaddyParamsFromInstall rebuilds the Params the Caddyfile is planned from, out
// of what the install keeps: its settings and the active sing-box config. The
// transport ports and secret paths are read back from the inbounds, so nothing
// the bootstrap knew once has to be stored a second time.
func CaddyParamsFromInstall(in Install, active map[string]interface{}) (Params, error) {
	host, portStr, err := net.SplitHostPort(in.Dest)
	if err != nil {
		return Params{}, fmt.Errorf("dest %q: %w", in.Dest, err)
	}
	destPort, err := strconv.Atoi(portStr)
	if err != nil {
		return Params{}, fmt.Errorf("dest %q: %w", in.Dest, err)
	}

	grpc := inboundOf(active, TagVlessGRPC)
	ws := inboundOf(active, TagTrojanWS)
	grpcPort, grpcName := inboundPort(grpc), inboundTransport(grpc, "service_name")
	wsPort, wsPath := inboundPort(ws), inboundTransport(ws, "path")
	if grpcPort == 0 || grpcName == "" || wsPort == 0 || wsPath == "" {
		return Params{}, fmt.Errorf("%w: the %s or %s inbound is missing or incomplete", ErrReplanSkipped, TagVlessGRPC, TagTrojanWS)
	}

	return Params{
		Domain:     in.Domain,
		DestHost:   host,
		DestPort:   destPort,
		StubRoot:   in.StubRoot,
		NaiveUsers: NaiveUsersPath(in.Caddyfile),
		Ports: Ports{
			Front:     in.FrontPort,
			VlessGRPC: grpcPort,
			TrojanWS:  wsPort,
			Panel:     in.PanelPort,
			WebFront:  in.WebFrontPort,
		},
		Paths: Paths{VlessGRPC: grpcName, TrojanWS: wsPath, Panel: in.PanelPath},
		ACME:  ACME{Email: in.ACMEEmail},
	}, nil
}

// RealityServerName is the name the front steals, read from the active config.
// Empty when there is no reality inbound.
func RealityServerName(active map[string]interface{}) string {
	tls, _ := inboundOf(active, TagVlessReality)["tls"].(map[string]interface{})
	name, _ := tls["server_name"].(string)
	return name
}

// inboundOf finds the inbound with this tag; nil when there is none.
func inboundOf(cfg map[string]interface{}, tag string) map[string]interface{} {
	arr, _ := cfg["inbounds"].([]interface{})
	for _, it := range arr {
		if ib, ok := it.(map[string]interface{}); ok && ib["tag"] == tag {
			return ib
		}
	}
	return nil
}

func inboundPort(ib map[string]interface{}) int {
	port, _ := ib["listen_port"].(float64)
	return int(port)
}

func inboundTransport(ib map[string]interface{}, key string) string {
	tr, _ := ib["transport"].(map[string]interface{})
	v, _ := tr[key].(string)
	return v
}

// SyncCaddyfile makes the Caddyfile at path match what p plans, and tells dest.
//
// A bad write must never take the site down, so the planned text is checked by
// dest itself (Caddy's /adapt) before it replaces anything: a refusal returns
// with the file untouched. Only an unreachable dest skips the check — then there
// is nothing running to take down, and the file is written for dest's next start.
//
// changed reports whether the file now on disk differs from the one before. A
// refused reload restores the previous file and reports false; only a failed
// restore leaves changed true, since the refused file is then still there.
func SyncCaddyfile(p Params, path string) (changed bool, err error) {
	existing, err := os.ReadFile(path)
	existed := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	// The staging CA is an operator's hand edit that is not a setting anywhere;
	// carrying it over is what keeps a trial install from flipping to the
	// production CA on the next start.
	p.ACME.Staging = strings.Contains(string(existing), stagingCA)
	rendered, err := PlanCaddyfile(p)
	if err != nil {
		return false, err
	}
	if rendered == string(existing) {
		return false, nil
	}

	admin := caddyAdminAddr()
	if err := adaptCaddyfile(admin, rendered); err != nil {
		if errors.Is(err, ErrDestUnreachable) {
			log.Printf("bootstrap: dest did not answer on %s, writing the Caddyfile unchecked", admin)
		} else {
			return false, err
		}
	}

	if err := util.WriteFileAtomic(path, []byte(rendered), 0644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	if err := ReloadCaddy(path); err != nil {
		if errors.Is(err, ErrDestUnreachable) {
			log.Printf("bootstrap: %v; the new Caddyfile applies when dest starts", err)
			return true, nil
		}
		// /adapt only parses; /load can still refuse (provisioning, imports,
		// listeners). Caddy rolls back in memory, but dest's next start reads the
		// file, so a refused Caddyfile must not stay on disk.
		var restoreErr error
		if existed {
			restoreErr = util.WriteFileAtomic(path, existing, 0644)
		} else {
			restoreErr = os.Remove(path)
		}
		if restoreErr != nil {
			return true, fmt.Errorf("%w; restoring the previous Caddyfile also failed, the refused one is still on disk: %v", err, restoreErr)
		}
		return false, err
	}
	return true, nil
}

// adaptCaddyfile asks dest to parse text without running it. An ErrDestUnreachable
// error means no answer; any other error is dest's refusal.
func adaptCaddyfile(admin, text string) error {
	req, err := http.NewRequest(http.MethodPost, "http://"+admin+"/adapt", strings.NewReader(text))
	if err != nil {
		return fmt.Errorf("build the check request for dest: %w", err)
	}
	req.Header.Set("Content-Type", "text/caddyfile")
	resp, err := (&http.Client{Timeout: caddyReloadTimeout}).Do(req)
	if err != nil {
		return fmt.Errorf("dest did not answer on %s: %w: %w", admin, ErrDestUnreachable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		return nil
	}
	reason, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	return fmt.Errorf("dest refused the planned Caddyfile (%s): %s", resp.Status, bytes.TrimSpace(reason))
}
