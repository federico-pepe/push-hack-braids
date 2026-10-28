package main

// pushhack.go — this hack's link to the PushHack Remote Script in Live
// (127.0.0.1:7704, JSON lines; reference: ableton-push-hack
// docs/pushhack-remote-script.md). Two jobs:
//
//   - runLiveMeter follows the level of Braids' own audio track in Live (the
//     "meters" topic) so the Volume fader shows what Live actually hears.
//   - ensureBraidsTracks makes the two tracks Braids needs, with their
//     routing: an audio track that reads Braids' audio from the virtual card,
//     and a MIDI track whose output is "Braids MIDI In".

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	pushHackAddr = "127.0.0.1:7704"

	// The names of the two tracks in the user's Set. The audio track is the one
	// whose meter the Volume fader shows.
	braidsAudioTrack = "Braids"
	braidsMIDITrack  = "Braids MIDI"

	phCallTimeout = 5 * time.Second
)

// phError is a failed request: the script's error code and its detail.
type phError struct {
	Code string
	Data json.RawMessage
}

func (e *phError) Error() string { return e.Code }

// phCall sends one request over its own connection and returns the reply's
// data. A failed request is a *phError.
func phCall(ns, cmd string, args map[string]any) (json.RawMessage, error) {
	req := map[string]any{"id": 1, "ns": ns, "cmd": cmd}
	for k, v := range args {
		req[k] = v
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("tcp", pushHackAddr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(phCallTimeout))
	if _, err := conn.Write(append(b, '\n')); err != nil {
		return nil, err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if len(line) == 0 && err != nil {
		return nil, err
	}
	var r struct {
		OK    *bool           `json:"ok"`
		Error string          `json:"error"`
		Data  json.RawMessage `json:"data"`
	}
	if json.Unmarshal(line, &r) != nil || r.OK == nil {
		return nil, errors.New("the Remote Script does not answer like PushHack: install or update Push Hack")
	}
	if !*r.OK {
		return nil, &phError{Code: r.Error, Data: r.Data}
	}
	return r.Data, nil
}

// ---- Live's meter ----

// liveTrack is the part of a "tracks" topic entry this hack reads.
type liveTrack struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// braidsTrackIndex is the index of the audio track called "Braids", or -1.
func braidsTrackIndex(tracks []liveTrack) int {
	for i, t := range tracks {
		if t.Name == braidsAudioTrack && t.Kind == "audio" {
			return i
		}
	}
	return -1
}

// meterOf picks track idx out of a "meters" topic's tracks: the louder of
// left and right. ok is false if idx is not in the list.
func meterOf(meters [][]float64, idx int) (float64, bool) {
	if idx < 0 || idx >= len(meters) || len(meters[idx]) < 2 {
		return 0, false
	}
	return max(meters[idx][0], meters[idx][1]), true
}

// runLiveMeter keeps a subscription to "tracks" and "meters" and feeds the
// level of the Braids audio track into level. It reconnects with a back-off
// and clears the live value whenever the link is down, so the fader falls
// back to Braids' own level.
func runLiveMeter(level *levelMeter, shutdown <-chan struct{}) {
	wait := 2 * time.Second
	last := ""
	for {
		err := followMeter(level, shutdown)
		level.clearLive()
		if msg := fmt.Sprint(err); msg != last {
			log.Printf("pushhack: %s", msg)
			last = msg
		}
		select {
		case <-shutdown:
			return
		case <-time.After(wait):
		}
		wait = min(wait*2, 15*time.Second)
	}
}

func followMeter(level *levelMeter, shutdown <-chan struct{}) error {
	conn, err := net.DialTimeout("tcp", pushHackAddr, 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	go func() { <-shutdown; conn.Close() }()

	rd := bufio.NewReaderSize(conn, 256<<10)
	call := func(req string) (map[string]json.RawMessage, error) {
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := conn.Write([]byte(req + "\n")); err != nil {
			return nil, err
		}
		line, err := rd.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(line, &m) != nil {
			return nil, errors.New("the Remote Script does not answer like PushHack")
		}
		return m, nil
	}
	hello, err := call(`{"id":1,"cmd":"hello"}`)
	if err != nil {
		return err
	}
	var hd struct {
		Topics []string `json:"topics"`
	}
	_ = json.Unmarshal(hello["data"], &hd)
	if !contains(hd.Topics, "meters") || !contains(hd.Topics, "tracks") {
		return errors.New("PushHack is too old for the meter (needs 0.3.0 or later)")
	}
	if _, err := call(`{"id":2,"cmd":"subscribe","topics":["tracks","meters"]}`); err != nil {
		return err
	}
	log.Printf("pushhack: following the level of the %q track in Live", braidsAudioTrack)

	idx := -1
	for {
		// A quiet socket is normal (events only on change). Ask hello now
		// and then: a dead socket fails the write.
		_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		line, err := rd.ReadBytes('\n')
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				// call() left a write deadline from the handshake: set a fresh one.
				_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
				if _, werr := conn.Write([]byte(`{"id":3,"cmd":"hello"}` + "\n")); werr != nil {
					return werr
				}
				continue
			}
			return err
		}
		var m struct {
			T     string          `json:"t"`
			Topic string          `json:"topic"`
			Data  json.RawMessage `json:"data"`
		}
		if json.Unmarshal(line, &m) != nil || m.T != "event" {
			continue
		}
		switch m.Topic {
		case "tracks":
			var d struct {
				Tracks []liveTrack `json:"tracks"`
			}
			if json.Unmarshal(m.Data, &d) == nil {
				idx = braidsTrackIndex(d.Tracks)
				if idx < 0 {
					level.clearLive()
				}
			}
		case "meters":
			var d struct {
				Tracks [][]float64 `json:"tracks"`
			}
			if json.Unmarshal(m.Data, &d) == nil {
				if v, ok := meterOf(d.Tracks, idx); ok {
					level.setLive(v)
				}
			}
		}
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// ---- making the tracks ----

var stereoPairRe = regexp.MustCompile(`^(\d+)/(\d+)$`)

// pairName is the name Live gives a stereo channel pair: offset 2 (channels
// 3 and 4, counting from 1) is "3/4".
func pairName(offset int) string { return fmt.Sprintf("%d/%d", offset+1, offset+2) }

// parsePair turns "3/4" back into the 0-based offset 2. ok is false for a
// mono name ("3") or a pair that is not two neighbours ("3/5").
func parsePair(name string) (offset int, ok bool) {
	m := stereoPairRe.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	a, _ := strconv.Atoi(m[1])
	b, _ := strconv.Atoi(m[2])
	if a < 1 || b != a+1 {
		return 0, false
	}
	return a - 1, true
}

// onlyStereoPair: if exactly one of the names Live offers is a stereo pair,
// that pair. A user who enabled one pair of the virtual card gets exactly that.
func onlyStereoPair(options []string) (string, bool) {
	found := ""
	for _, o := range options {
		if _, ok := parsePair(o); ok {
			if found != "" {
				return "", false
			}
			found = o
		}
	}
	return found, found != ""
}

// routingOptions reads the names Live offered out of a bad_routing error.
func routingOptions(e *phError) (what string, options []string) {
	var d struct {
		What    string   `json:"what"`
		Options []string `json:"options"`
	}
	_ = json.Unmarshal(e.Data, &d)
	return d.What, d.Options
}

// tracksResult is what the "Create tracks" button reports.
type tracksResult struct {
	Lines []string `json:"lines"`
}

// ensureBraidsTracks makes the audio and the MIDI track in Live, and sets
// their routing. channel/setChannel are Braids' own channel pair (0-based
// offset) and the way to change it: if Live offers exactly one stereo pair on
// the input, that is the pair the user enabled, so Braids is switched to it.
func ensureBraidsTracks(channelOffset int, setChannelOffset func(int) error) (tracksResult, error) {
	var res tracksResult
	pair := pairName(channelOffset)
	audio := map[string]any{
		"kind": "audio", "name": braidsAudioTrack,
		"input_type": "Ext. In", "input_channel": []string{pair},
		"output_type": "Main", "monitoring": 0,
	}
	data, err := phCall("track", "ensure", audio)
	var pe *phError
	if errors.As(err, &pe) && pe.Code == "bad_routing" {
		what, options := routingOptions(pe)
		if what == "channel" {
			if only, ok := onlyStereoPair(options); ok {
				off, _ := parsePair(only)
				if serr := setChannelOffset(off); serr != nil {
					return res, fmt.Errorf("switching Braids to channels %s: %w", only, serr)
				}
				res.Lines = append(res.Lines, fmt.Sprintf("Braids now writes to channels %s, the pair Live offers.", only))
				audio["input_channel"] = []string{only}
				data, err = phCall("track", "ensure", audio)
			} else {
				return res, fmt.Errorf("Live does not offer channels %s on Ext. In (it offers: %s). Enable the Push Hack Virtual Audio card in Push's audio preferences, then set Braids' channel pair to match", pair, strings.Join(options, ", "))
			}
		}
	}
	if err != nil {
		return res, friendlyPHError("audio track", err)
	}
	a := parseEnsure(data)
	res.Lines = append(res.Lines, trackLine(braidsAudioTrack, a.Created))

	data, err = phCall("track", "ensure", map[string]any{
		"kind": "midi", "name": braidsMIDITrack,
		"input_type": "All Ins", "output_type": "Braids MIDI In",
	})
	if err != nil {
		return res, friendlyPHError("MIDI track", err)
	}
	m := parseEnsure(data)
	res.Lines = append(res.Lines, trackLine(braidsMIDITrack, m.Created))
	if m.Created {
		// A new MIDI track is the one to play: select and arm it.
		_, _ = phCall("track", "select", map[string]any{"track": m.Track})
		_, _ = phCall("track", "set", map[string]any{"track": m.Track, "arm": true})
		res.Lines = append(res.Lines, "Selected and armed. Press the pads to play Braids.")
	}
	return res, nil
}

type ensureReply struct {
	Track   any  `json:"track"`
	Created bool `json:"created"`
}

func parseEnsure(data json.RawMessage) ensureReply {
	var r ensureReply
	_ = json.Unmarshal(data, &r)
	return r
}

func trackLine(name string, created bool) string {
	if created {
		return fmt.Sprintf("Created the %q track.", name)
	}
	return fmt.Sprintf("The %q track was already there; routing set.", name)
}

// friendlyPHError turns a failed request into a sentence for the web UI.
func friendlyPHError(what string, err error) error {
	var pe *phError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "bad_routing":
			_, options := routingOptions(pe)
			return fmt.Errorf("%s: Live does not offer that routing (it offers: %s)", what, strings.Join(options, ", "))
		case "wrong_track_kind":
			return fmt.Errorf("%s: a track with that name exists but is the wrong kind; rename it in Live", what)
		case "unknown_ns", "unknown_cmd":
			return fmt.Errorf("%s: the PushHack Remote Script is too old (needs 0.5.0 or later); update Push Hack and restart Live", what)
		}
		return fmt.Errorf("%s: %s", what, pe.Code)
	}
	return fmt.Errorf("%s: %w (is the PushHack Remote Script selected in Live?)", what, err)
}
