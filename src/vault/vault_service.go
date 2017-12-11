package vault

import (
	"context"
	"errors"
	stdjwt "github.com/dgrijalva/jwt-go"
	"github.com/go-kit/kit/auth/jwt"
	"github.com/go-kit/kit/circuitbreaker"
	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/ratelimit"
	"github.com/go-kit/kit/tracing/opentracing"
	stdopentracing "github.com/opentracing/opentracing-go"
	"github.com/sony/gobreaker"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/time/rate"
	"time"
)

type Service interface {
	Hash(ctx context.Context, password string) (string, error)
	Validate(ctx context.Context, password, hash string) (bool, error)
	HealthCheck() bool
}

func NewVaultService() Service {
	return newVaultService{}
}

type ServiceMiddleware func(service Service) Service

type newVaultService struct {}

type hashRequest struct {
	Password string `json:"password"`
}

type hashResponse struct {
	Hash string `json:"hash"`
	Err string `json:"err, omitempty"`
}

type validateRequest struct {
	Password string `json:"password"`
	Hash string `json:"hash"`
}

type validateResponse struct {
	Valid bool `json:"valid"`
	Err string `json:"err, omitempty"`
}

//TODO Create health logic, check free memory, disk space
type healthRequest struct {}

type healthResponse struct {
	Status bool `json:"status"`
}

func (newVaultService) Hash(ctx context.Context, password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func (newVaultService) Validate(ctx context.Context, password, hash string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		return false, err
	}
	return true, nil
}

func (newVaultService) HealthCheck() bool {
	return true
}

type Endpoints struct {
	HashEnpoint endpoint.Endpoint
	ValidateEndpoint endpoint.Endpoint
	VaultHealtEndpoint endpoint.Endpoint
}

func (e Endpoints) Hash(ctx context.Context, password string) (string, error) {
	req := hashRequest {Password: password}
	resp, err := e.HashEnpoint(ctx, req)
	if err != nil {
		return "", err
	}
	hashResp := resp.(hashResponse)
	if hashResp.Err != "" {
		return "", errors.New(hashResp.Err)
	}
	return hashResp.Hash, nil
}

func (e Endpoints) Validate(ctx context.Context, password, hash string) (bool, error) {
	req := validateRequest{Password: password, Hash: hash}
	resp, err := e.ValidateEndpoint(ctx, req)
	if err != nil {
		return false, err
	}
	validateResp := resp.(validateResponse)
	if validateResp.Err != "" {
		return false, errors.New(validateResp.Err)
	}
	return validateResp.Valid, nil
}

func MakeHashEndpoint(svc Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (response interface{}, err error) {
		req := request.(hashRequest)
		v, err := svc.Hash(ctx, req.Password)
		if err != nil {
			return hashResponse{Hash:v, Err: err.Error()}, err
		}
		return hashResponse{Hash:v, Err: ""}, nil
	}
}

func MakeValidateEndpoint(svc Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (response interface{}, err error) {
		req := request.(validateRequest)
		v, err := svc.Validate(ctx, req.Password, req.Hash)
		if err != nil {
			return validateResponse{Valid: v, Err: err.Error()}, err
		}
		return validateResponse{Valid: v, Err: ""}, nil
	}
}
//TODO correct health request
func MakeHealtEndpoint(svc Service) endpoint.Endpoint {
	return func(ctx context.Context, request interface{}) (response interface{}, err error) {
		//req := request.(healthRequest)
		v := svc.HealthCheck()
		return healthResponse{Status: v}, nil
	}
}

func NewEndpoints(svc Service, logger log.Logger, trace stdopentracing.Tracer) Endpoints {
	kf := func(token *stdjwt.Token) (interface{}, error) { return []byte("%kxkstXG%@uEG4^fj_gt8*XK?tzG@ddY#+wAd"), nil }
	var hashEndpoint endpoint.Endpoint
	{
		hashEndpoint = MakeHashEndpoint(svc)
		hashEndpoint = jwt.NewParser(kf, stdjwt.SigningMethodHS256, jwt.StandardClaimsFactory)(hashEndpoint)
		hashEndpoint = ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), 1))(hashEndpoint)
		hashEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{}))(hashEndpoint)
		hashEndpoint = opentracing.TraceServer(trace, "hash")(hashEndpoint)
		hashEndpoint = LoggingMiddlware(log.With(logger,"method", "hash"))(hashEndpoint)
	}
	var validateEndpoint endpoint.Endpoint
	{
		validateEndpoint = MakeValidateEndpoint(svc)
		validateEndpoint = jwt.NewParser(kf, stdjwt.SigningMethodHS256, jwt.StandardClaimsFactory)(validateEndpoint)
		validateEndpoint = ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), 1))(validateEndpoint)
		validateEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{}))(validateEndpoint)
		validateEndpoint = opentracing.TraceServer(trace, "validate")(validateEndpoint)
		validateEndpoint = LoggingMiddlware(log.With(logger,"method", "validate"))(validateEndpoint)
	}
	var healthEndpoint endpoint.Endpoint
	{
		healthEndpoint = MakeHealtEndpoint(svc)
		healthEndpoint = ratelimit.NewErroringLimiter(rate.NewLimiter(rate.Every(time.Second), 1))(healthEndpoint)
		healthEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{}))(healthEndpoint)
		healthEndpoint = opentracing.TraceServer(trace, "health")(healthEndpoint)
		healthEndpoint = LoggingMiddlware(log.With(logger,"method", "health"))(healthEndpoint)
	}

	return Endpoints{
		HashEnpoint: hashEndpoint,
		ValidateEndpoint: validateEndpoint,
		VaultHealtEndpoint: healthEndpoint,
	}
}

















