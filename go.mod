module github.com/nakagami/grdpwasm

go 1.26.3

require (
	github.com/gorilla/websocket v1.5.3
	github.com/nakagami/grdp v0.9.7
)

require (
	github.com/lunixbochs/struc v0.0.0-20200707160740-784aaebc1d40 // indirect
	golang.org/x/crypto v0.0.0-20220622213112-05595931fe9d // indirect
)

// Local vendor fork: fix the codecPlanar constant (0x0004 -> 0x000A), otherwise xrdp planar frames (codecId=0x0A) cannot be decoded -> black screen
replace github.com/nakagami/grdp => ./third_party/grdp
