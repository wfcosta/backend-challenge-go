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

type Deduplicador interface {
	Registrar(context.Context, string, string, string) (bool, error)
	Concluir(context.Context, string, string) error
}

type ClienteMensagens interface {
	ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(context.Context, *sqs.DeleteMessageInput, ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

type Consumidor struct {
	Cliente        ClienteMensagens
	FilaURL        string
	Tratador       TratadorMensagem
	Inbox          Deduplicador
	NomeConsumidor string
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
			if c.Inbox != nil {
				novo, err := c.Inbox.Registrar(ctx, c.NomeConsumidor, aws.ToString(mensagem.MessageId), aws.ToString(mensagem.Body))
				if err != nil {
					slog.Error("falha ao registrar inbox", "erro", err)
					continue
				}
				if !novo {
					_, _ = c.Cliente.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(c.FilaURL), ReceiptHandle: mensagem.ReceiptHandle})
					continue
				}
			}
			if err := c.Tratador.Tratar(ctx, aws.ToString(mensagem.MessageId), aws.ToString(mensagem.Body)); err != nil {
				slog.Error("falha ao tratar mensagem SQS", "messageId", aws.ToString(mensagem.MessageId), "erro", err)
				continue
			}
			if c.Inbox != nil {
				if err := c.Inbox.Concluir(ctx, c.NomeConsumidor, aws.ToString(mensagem.MessageId)); err != nil {
					slog.Error("falha ao concluir inbox", "erro", err)
					continue
				}
			}
			_, err := c.Cliente.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: aws.String(c.FilaURL), ReceiptHandle: mensagem.ReceiptHandle})
			if err != nil {
				slog.Error("falha ao remover mensagem SQS", "messageId", aws.ToString(mensagem.MessageId), "erro", err)
			}
		}
	}
}
