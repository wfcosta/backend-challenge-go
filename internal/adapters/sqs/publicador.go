package sqs

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/wfcosta/backend-challenge-go/internal/worker"
)

type Publicador struct {
	Cliente *sqs.Client
	FilaURL string
}

func (p Publicador) Publicar(ctx context.Context, evento worker.EventoPendente) error {
	_, err := p.Cliente.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               aws.String(p.FilaURL),
		MessageBody:            aws.String(string(evento.Payload)),
		MessageGroupId:         aws.String(evento.Tipo),
		MessageDeduplicationId: aws.String(evento.ID),
	})
	return err
}
