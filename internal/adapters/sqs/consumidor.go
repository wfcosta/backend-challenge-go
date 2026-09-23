package sqs

import (
	"context"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type TratadorMensagem interface {
	Tratar(context.Context, string, string) error
}

type Consumidor struct {
	Cliente  *sqs.Client
	FilaURL  string
	Tratador TratadorMensagem
}

func (c Consumidor) Executar(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		resposta, err := c.Cliente.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:              aws.String(c.FilaURL),
			MaxNumberOfMessages:   10,
			WaitTimeSeconds:       10,
			MessageAttributeNames: []string{"All"},
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("falha ao receber mensagem SQS", "erro", err)
			continue
		}
		for _, mensagem := range resposta.Messages {
			if mensagem.MessageId == nil || mensagem.ReceiptHandle == nil || mensagem.Body == nil {
				continue
			}
			if err := c.Tratador.Tratar(ctx, aws.ToString(mensagem.MessageId), aws.ToString(mensagem.Body)); err != nil {
				slog.Error("falha ao tratar mensagem SQS", "messageId", aws.ToString(mensagem.MessageId), "erro", err)
				continue
			}
			_, err := c.Cliente.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(c.FilaURL), ReceiptHandle: mensagem.ReceiptHandle})
			if err != nil {
				slog.Error("falha ao remover mensagem SQS", "messageId", aws.ToString(mensagem.MessageId), "erro", err)
			}
		}
	}
}
