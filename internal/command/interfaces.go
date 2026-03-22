package command

import (
	"context"

	"kalita/internal/eventcore"
)

type AdmissionPolicy interface {
	Admit(ctx context.Context, cmd eventcore.Command) error
}

type CommandBus interface {
	Submit(ctx context.Context, cmd eventcore.Command) (eventcore.Command, error)
}
