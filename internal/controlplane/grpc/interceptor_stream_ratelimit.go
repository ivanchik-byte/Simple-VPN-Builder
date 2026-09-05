package grpc

import (
	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type rateLimitedServerStream struct {
	grpc.ServerStream
	limiter *rate.Limiter
}

func (s *rateLimitedServerStream) RecvMsg(m any) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}
	if !s.limiter.Allow() {
		return status.Errorf(codes.ResourceExhausted, "message rate limit exceeded on stream")
	}
	return nil
}

// StreamRateLimitInterceptor creates a Token Bucket rate limiter per active stream.
// It limits incoming messages inside the stream (e.g. max r queries/sec with burst b).
func StreamRateLimitInterceptor(r rate.Limit, b int) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		limiter := rate.NewLimiter(r, b)
		wrapped := &rateLimitedServerStream{
			ServerStream: ss,
			limiter:      limiter,
		}
		return handler(srv, wrapped)
	}
}
