module example.com/orders

go 1.25

require (
	github.com/google/uuid v1.6.0
	golang.org/x/sync v0.10.0
	golang.org/x/text v0.20.0
)

replace github.com/google/uuid => ../uuid-fork
