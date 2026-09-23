package sqs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

func TestConsumidorPodeSerCancelado(t *testing.T) {
	var c Consumidor
	if c.FilaURL != "" {
		t.Fatal("fila inicial deveria estar vazia")
	}
}

type clienteMensagensTeste struct {
	mu        sync.Mutex
	mensagens []types.Message
	deletes   int
}

func (c *clienteMensagensTeste) ReceiveMessage(ctx context.Context, _ *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	c.mu.Lock()
	if len(c.mensagens) > 0 {
		mensagem := c.mensagens[0]
		c.mensagens = c.mensagens[1:]
		c.mu.Unlock()
		return &sqs.ReceiveMessageOutput{Messages: []types.Message{mensagem}}, nil
	}
	c.mu.Unlock()
	<-ctx.Done()
	return nil, ctx.Err()
}

func (c *clienteMensagensTeste) DeleteMessage(_ context.Context, _ *sqs.DeleteMessageInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deletes++
	return &sqs.DeleteMessageOutput{}, nil
}

type tratadorTeste struct{ err error }

func (t tratadorTeste) Tratar(context.Context, string, string) error { return t.err }

type inboxTeste struct{ novo bool }

func (i inboxTeste) Registrar(context.Context, string, string, string) (bool, error) {
	return i.novo, nil
}
func (i inboxTeste) Concluir(context.Context, string, string) error { return nil }

func TestConsumidorNaoConfirmaMensagemQuandoTratamentoFalha(t *testing.T) {
	cliente := &clienteMensagensTeste{mensagens: []types.Message{{MessageId: aws.String("m1"), ReceiptHandle: aws.String("r1"), Body: aws.String("{}")}}}
	ctx, cancelar := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelar()
	Consumidor{Cliente: cliente, FilaURL: "fila", Tratador: tratadorTeste{err: errors.New("falha")}, Inbox: inboxTeste{novo: true}}.Executar(ctx)
	cliente.mu.Lock()
	deletes := cliente.deletes
	cliente.mu.Unlock()
	if deletes != 0 {
		t.Fatalf("mensagem com falha não deveria ser removida: %d", deletes)
	}
}

func TestConsumidorRemoveMensagemDuplicada(t *testing.T) {
	cliente := &clienteMensagensTeste{mensagens: []types.Message{{MessageId: aws.String("m1"), ReceiptHandle: aws.String("r1"), Body: aws.String("{}")}}}
	ctx, cancelar := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelar()
	Consumidor{Cliente: cliente, FilaURL: "fila", Tratador: tratadorTeste{}, Inbox: inboxTeste{novo: false}}.Executar(ctx)
	cliente.mu.Lock()
	deletes := cliente.deletes
	cliente.mu.Unlock()
	if deletes != 1 {
		t.Fatalf("duplicata deveria ser removida: %d", deletes)
	}
}
