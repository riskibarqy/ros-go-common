package grpcapi

import (
	"google.golang.org/grpc/status"

	"ros-go-common/failure"
	"ros-go-common/httpapi"
)

// ProblemFromGRPCStatus requires an application-owned contextual classifier.
// The upstream status message is deliberately never copied into the problem.
func ProblemFromGRPCStatus(s *status.Status, classify func(*status.Status) failure.Kind) httpapi.ProblemDetails {
	return httpapi.NewProblem(classify(s))
}
