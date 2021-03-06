package main

import (
	"context"
	"flag"
	"net"
	"net/http"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/logrus"
	"github.com/grpc-ecosystem/go-grpc-middleware/validator"
	toolkit_auth "github.com/infobloxopen/atlas-app-toolkit/mw/auth"
	"github.com/jinzhu/gorm"
	"github.com/sirupsen/logrus"
	"google.golang.org/grpc"

	"github.com/infobloxopen/atlas-app-toolkit/health"
	"github.com/infobloxopen/atlas-contacts-app/cmd/config"
	pb "github.com/infobloxopen/atlas-contacts-app/pb/contacts"
	svc "github.com/infobloxopen/atlas-contacts-app/svc/contacts"
)

var (
	Address       string
	HealthAddress string
	Dsn           string
	AuthzAddr     string
)

func main() {
	logger := logrus.New()
	// create new tcp listenerf
	ln, err := net.Listen("tcp", Address)
	if err != nil {
		logger.Fatalln(err)
	}

	interceptors := []grpc.UnaryServerInterceptor{
		// validation interceptor
		grpc_validator.UnaryServerInterceptor(),
		// validation interceptor
		grpc_logrus.UnaryServerInterceptor(logrus.NewEntry(logger)),
	}
	// add authorization interceptor if authz service address is provided
	if AuthzAddr != "" {
		interceptors = append(interceptors,
			// authorization interceptor
			toolkit_auth.DefaultAuthInterceptor(AuthzAddr),
		)
	}
	middleware := grpc_middleware.ChainUnaryServer(interceptors...)
	// create new gRPC server with middleware chain
	server := grpc.NewServer(
		grpc.UnaryInterceptor(
			middleware,
		),
	)

	healthChecker := health.NewChecksHandler("/healthz", "/ready")
	healthChecker.AddReadiness("DB ready check", dbReady)
	go http.ListenAndServe(HealthAddress, healthChecker.Handler())

	// waiting for database is available.
	dbCheckContext, cancel := context.WithTimeout(context.Background(), time.Minute*5)

	for {
		select {
		case <-time.After(time.Second * 3):
			if err = dbReady(); err != nil {
				continue
			}
			cancel()
		case <-dbCheckContext.Done():
			cancel()
		}
		break
	}
	if err != nil {
		logger.Fatalf("Couldn't initialize server due to: %v\n", err)
	}

	// register service implementation with the grpc server
	s, err := svc.NewBasicServer(Dsn)
	if err != nil {
		logger.Fatalln(err)
	}
	pb.RegisterContactsServer(server, s)

	if err := server.Serve(ln); err != nil {
		logger.Fatalln(err)
	}
}

func init() {
	// default server address; optionally set via command-line flags
	flag.StringVar(&Address, "address", config.SERVER_ADDRESS, "the gRPC server address")
	flag.StringVar(&HealthAddress, "health", "0.0.0.0:8089", "Address for health checking")
	flag.StringVar(&Dsn, "dsn", "", "")
	flag.StringVar(&AuthzAddr, "authz", "", "address of the authorization service")
	flag.Parse()
}

func dbReady() error {
	db, err := gorm.Open("postgres", Dsn)
	if err != nil {
		return err
	}
	db.Close()
	return nil
}
