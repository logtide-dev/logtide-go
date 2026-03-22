module example-basic

go 1.23.0

require github.com/logtide-dev/logtide-sdk-go v0.1.0

require (
	go.opentelemetry.io/otel v1.38.0 // indirect
	go.opentelemetry.io/otel/trace v1.38.0 // indirect
)

replace github.com/logtide-dev/logtide-sdk-go => ../..
