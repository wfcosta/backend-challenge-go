package sqs

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

func NovoCliente(ctx context.Context, regiao, endpoint string) (*sqs.Client, error) {
	opcoes := []func(*config.LoadOptions) error{config.WithRegion(regiao)}
	if endpoint != "" {
		opcoes = append(opcoes, config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(func(string, string, ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{URL: endpoint, SigningRegion: regiao}, nil
		})))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opcoes...)
	if err != nil {
		return nil, err
	}
	return sqs.NewFromConfig(cfg), nil
}
