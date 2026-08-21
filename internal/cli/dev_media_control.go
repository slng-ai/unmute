package cli

import (
	"crypto/subtle"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Call control on the media websocket plane: the carrier's REST API, served on
// loopback (T067).
//
// A cold transfer on the cloud-websocket route is not a message on the media
// stream. The agent replaces the live call's document at the carrier, over
// HTTPS, and the carrier then does the announcing, the dialling and the
// hanging up. With no carrier in the run that request either leaves the machine
// or fails having already left it, which gate P2 forbids either way, and the
// transfer the caller was promised never happens. So the stand-in serves the
// endpoint and carries the document out itself.
//
// Only the one route the emitted code actually posts to is served. The other
// call-control caller in the templates is the daily-sip route's
// _forward_carrier_call, and that route has no local plane at all: it keeps its
// refusal (FR-007, T070). Serving an endpoint nothing on this plane can reach
// would be a promise the refusal contradicts.

// carrierCallPath is the carrier's own call-control path. Matched as a prefix
// with a suffix check rather than by pattern, because the account identifier in
// the middle is minted per run and the call id is minted per call: what makes
// this the right request is its shape, which is all the stand-in can check.
const carrierCallPath = "/2010-04-01/Accounts/"

// transferInstruction is a carrier document, read as the three things the
// stand-in can act on. The emitted cold transfer produces exactly this shape:
// announce, dial with a ring timeout, then end the original call.
type transferInstruction struct {
	Say         string
	Destination string
	RingTimeout time.Duration
	// Hangup is whether the document ends the original call after the dial. It
	// is what tells a transfer from a redirect, and the plane must not end a
	// caller's leg the document did not ask it to end.
	Hangup bool
}

// parseTransferDocument reads a carrier call document. Only the verbs this
// plane can carry out are read; an unknown verb is not an error, because a
// document the stand-in only partly understands still transfers the call, and
// failing the request would report a broken product instead of a thin stand-in.
func parseTransferDocument(document string) (transferInstruction, error) {
	var parsed struct {
		XMLName xml.Name `xml:"Response"`
		Say     string   `xml:"Say"`
		Dial    *struct {
			Number  string `xml:",chardata"`
			SIP     string `xml:"Sip"`
			Timeout string `xml:"timeout,attr"`
		} `xml:"Dial"`
		Hangup *struct{} `xml:"Hangup"`
	}
	if err := xml.Unmarshal([]byte(document), &parsed); err != nil {
		return transferInstruction{}, fmt.Errorf("the agent's call document is not XML: %w", err)
	}
	if parsed.Dial == nil {
		return transferInstruction{}, fmt.Errorf("the agent's call document has no <Dial>, so there is "+
			"nothing to transfer to: %s", document)
	}
	instruction := transferInstruction{
		Say:    strings.TrimSpace(parsed.Say),
		Hangup: parsed.Hangup != nil,
	}
	instruction.Destination = strings.TrimSpace(parsed.Dial.SIP)
	if instruction.Destination == "" {
		instruction.Destination = strings.TrimSpace(parsed.Dial.Number)
	}
	if instruction.Destination == "" {
		return transferInstruction{}, fmt.Errorf("the agent's <Dial> names no destination: %s", document)
	}
	if seconds, err := strconv.Atoi(parsed.Dial.Timeout); err == nil && seconds > 0 {
		instruction.RingTimeout = time.Duration(seconds) * time.Second
	}
	return instruction, nil
}

// outboundRequest is the agent asking the carrier to place a call, which on
// this plane means asking the stand-in to be the far end of one (T068).
type outboundRequest struct {
	CallID string
	To     string
	From   string
	// Address is the agent's own media stream address, read out of the document
	// the agent handed the carrier. The same address an inbound call's webhook
	// answers with, arriving by a different door.
	Address string
}

// mediaControl is the carrier's REST API for the length of one run: an address
// to give the agent, and a way to hand what arrives to the call it is about.
type mediaControl struct {
	// base is what the agent gets told the carrier's address is.
	base   string
	server *http.Server
	// accountSID and authToken are the run's own minted credentials, and every
	// request has to present them. The real API authenticates, so this is more
	// faithful, and it is what makes the containerised case safe: an agent in a
	// container cannot reach a loopback listener, so that listener binds every
	// interface, and an unauthenticated endpoint that can cut a live call has no
	// business doing that.
	accountSID string
	authToken  string
	// port is the listener's own port, kept because base may carry a hostname
	// only the container can resolve.
	port string

	mu sync.Mutex
	// live maps a call id to the call waiting on instructions for it. Keyed by
	// call id rather than kept as one channel because a document is about one
	// call, and delivering it to a different call would be worse than dropping
	// it.
	live map[string]chan transferInstruction
	// outbound carries calls the agent asked to have placed. One deep: a run
	// places one call, and a second request while the first is unread is the
	// agent doing something this plane has no answer for.
	outbound chan outboundRequest
}

// containerHostName is how a container reaches the machine running it. Docker
// resolves it from the extra_hosts entry the emitted Compose file carries, using
// the special host-gateway value, which is the one portable spelling: Docker
// Desktop provides the name itself, and on Linux the entry is what creates it.
const containerHostName = "host.docker.internal"

// startMediaControl starts serving the carrier's API for one run.
//
// fromContainer says whether the agent runs in a container, and it changes two
// things together. A container cannot reach the host's loopback, so the listener
// binds every interface and the agent is told a name Docker resolves to the host
// rather than 127.0.0.1. That is a wider listener than a local plane wants, which
// is why every request must present this run's minted credentials: the port is
// ephemeral, the token dies with the run, and without it a machine on the same
// network could cut a call in progress.
//
// The port is always chosen by the operating system. A fixed one is a port that
// collides with the last run that crashed.
func startMediaControl(accountSID, authToken string, fromContainer bool) (*mediaControl, error) {
	host, advertise := "127.0.0.1", "127.0.0.1"
	if fromContainer {
		host, advertise = "0.0.0.0", containerHostName
	}
	listener, err := net.Listen("tcp", host+":0")
	if err != nil {
		return nil, fmt.Errorf("listen for the agent's call control: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	control := &mediaControl{
		base:       fmt.Sprintf("http://%s:%d", advertise, port),
		port:       strconv.Itoa(port),
		accountSID: accountSID,
		authToken:  authToken,
		live:       map[string]chan transferInstruction{},
		outbound:   make(chan outboundRequest, 1),
	}
	control.server = &http.Server{
		Handler:           control,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		// Serve until Close. A closed listener is how this ends, so the error it
		// returns then is the expected one and there is nobody left to tell.
		_ = control.server.Serve(listener)
	}()
	return control, nil
}

// close stops serving. Called on the way out of a run.
func (c *mediaControl) close() error {
	return c.server.Close()
}

// watch registers a call as able to receive instructions, and returns both the
// channel to read them from and the function that stops watching. Registered
// before the call is placed, because the agent can post before the stand-in has
// finished reading its first frame.
func (c *mediaControl) watch(callID string) (<-chan transferInstruction, func()) {
	// Buffered by one: the handler must not block on a call that is between
	// frames, and one document is all a call gets before its stream is cut.
	instructions := make(chan transferInstruction, 1)
	c.mu.Lock()
	c.live[callID] = instructions
	c.mu.Unlock()
	return instructions, func() {
		c.mu.Lock()
		delete(c.live, callID)
		c.mu.Unlock()
	}
}

// ServeHTTP is the carrier's call-control endpoint.
func (c *mediaControl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.authorised(r) {
		// The same status and shape the real API answers an unauthenticated
		// request with, so a misconfigured run reads the way it would there.
		c.fail(w, http.StatusUnauthorized, "authenticate with this run's own credentials")
		return
	}
	if r.Method == http.MethodPost && isCarrierCreatePath(r.URL.Path) {
		c.createCall(w, r)
		return
	}
	callID, ok := carrierCallID(r.URL.Path)
	if r.Method != http.MethodPost || !ok {
		// Said in the carrier's own vocabulary, because whoever reads this is
		// reading it out of an agent log next to the real API's messages. The
		// other two carriers' call creation lands here: their requests are
		// redirected so nothing leaves the machine, and this is where they are
		// told, in one sentence, that placing a call is not built for them.
		c.fail(w, http.StatusNotFound, "the local carrier stand-in serves "+
			"POST "+carrierCallPath+"{account}/Calls.json to place a call and "+
			"POST "+carrierCallPath+"{account}/Calls/{call}.json to control one. "+
			"No other carrier's call-creation dialect is implemented on the local "+
			"plane yet, so this request reached a stand-in that cannot answer it "+
			"rather than leaving your machine")
		return
	}
	if err := r.ParseForm(); err != nil {
		c.fail(w, http.StatusBadRequest, "the request body is not a form: "+err.Error())
		return
	}
	document := r.PostForm.Get("Twiml")
	if document == "" {
		c.fail(w, http.StatusBadRequest, "the request carries no Twiml parameter")
		return
	}
	instruction, err := parseTransferDocument(document)
	if err != nil {
		c.fail(w, http.StatusBadRequest, err.Error())
		return
	}
	c.mu.Lock()
	instructions := c.live[callID]
	c.mu.Unlock()
	if instructions == nil {
		// The agent is acting on a call this run has no record of. A 404 is what
		// the real API answers for an unknown call, and it is the honest answer:
		// the agent then reports the transfer as failed rather than announcing
		// one that cannot happen.
		c.fail(w, http.StatusNotFound, "no call "+callID+" is live on the local carrier stand-in")
		return
	}
	select {
	case instructions <- instruction:
	default:
		// A second document for a call whose first is still unread. The emitted
		// agent guards against starting two transfers, so this is the real API's
		// answer to a request that arrives too late to matter.
		c.fail(w, http.StatusConflict, "call "+callID+" is already being transferred")
		return
	}
	// The agent checks the status and nothing else, but a body shaped like the
	// real one costs three fields and means a reader comparing logs sees the
	// same thing twice.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sid": callID, "status": "in-progress", "direction": "inbound",
	})
}

// createCall is the agent asking for an outbound call. The stand-in answers at
// once and dials afterwards, because that is what the real API does: a created
// call is queued, not connected, and the agent is waiting on this response
// before it can serve anything.
func (c *mediaControl) createCall(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		c.fail(w, http.StatusBadRequest, "the request body is not a form: "+err.Error())
		return
	}
	// The document the agent handed the carrier is the same one an inbound call's
	// webhook answers with, so the same reader finds the stream address in it.
	address, err := mediaStreamAddress(r.PostForm.Get("Twiml"))
	if err != nil {
		c.fail(w, http.StatusBadRequest, "the call to place carries no stream address: "+err.Error())
		return
	}
	suffix, err := randomCredentialText(16)
	if err != nil {
		c.fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	request := outboundRequest{
		CallID:  "CA" + suffix,
		To:      r.PostForm.Get("To"),
		From:    r.PostForm.Get("From"),
		Address: address,
	}
	select {
	case c.outbound <- request:
	default:
		c.fail(w, http.StatusConflict, "a call is already being placed on the local carrier stand-in")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	// sid is the field the emitted agent reads off this. queued rather than
	// in-progress because nothing has been dialled yet, which is true.
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sid": request.CallID, "status": "queued",
		"to": request.To, "from": request.From, "direction": "outbound-api",
	})
}

// isCarrierCreatePath reports whether this is call creation rather than call
// control: the same collection, addressed without a call id because there is no
// call yet.
func isCarrierCreatePath(path string) bool {
	rest, ok := strings.CutPrefix(path, carrierCallPath)
	if !ok {
		return false
	}
	account, tail, ok := strings.Cut(rest, "/")
	return ok && account != "" && tail == "Calls.json"
}

// authorised checks the run's own minted credentials, in constant time so the
// token cannot be recovered a byte at a time. The emitted agents all send basic
// auth: the SDK from its constructor, the raw call-control request from an
// explicit BasicAuth. Verified against twilio 9.11.0 and the emitted template.
func (c *mediaControl) authorised(r *http.Request) bool {
	user, password, ok := r.BasicAuth()
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(user), []byte(c.accountSID)) == 1 &&
		subtle.ConstantTimeCompare([]byte(password), []byte(c.authToken)) == 1
}

// fail answers the way the carrier's API does, so a failure in the stand-in
// reads in an agent log the way a failure at the carrier would.
func (c *mediaControl) fail(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "message": message})
}

// carrierCallID pulls the call id out of the carrier's call-control path,
// reporting whether the path is that endpoint at all.
func carrierCallID(path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, carrierCallPath)
	if !ok {
		return "", false
	}
	_, rest, ok = strings.Cut(rest, "/Calls/")
	if !ok {
		return "", false
	}
	callID, ok := strings.CutSuffix(rest, ".json")
	if !ok || callID == "" || strings.Contains(callID, "/") {
		return "", false
	}
	return callID, true
}
