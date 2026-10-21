// Este archivo solo existe para que `go test ./...` no recorra web/node_modules
// (algunos paquetes de npm traen archivos .go). No es un módulo real.
module pulse-web-ignore

go 1.27.3
