package event

// ETP2PQriPeerConnectedEvent occurs after a qri peer has connected to this node
// the payload will be a fully hydrated *profile.Profile from
// "github.com/qri-io/qri/repo/profile"
var ETP2PQriPeerConnectedEvent = Topic("p2p:QriPeerConnected")
