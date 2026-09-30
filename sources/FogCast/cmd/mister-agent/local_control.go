package main

import (
	"context"
	"errors"

	"github.com/DeanoC/FogCast/internal/localcores"
	"github.com/DeanoC/FogCast/internal/misterruntime"
)

type nativeLocalRuntime struct {
	runtime *misterruntime.Runtime
}

func (n nativeLocalRuntime) LoadCore(ctx context.Context, path, packageID string) error {
	if n.runtime == nil {
		return errors.New("runtime is closed")
	}
	response, err := n.runtime.LoadInstalledCoreOwned(ctx, ctx, path, packageID)
	if err != nil {
		return err
	}
	if !response.OK {
		return errors.New("installed core load failed")
	}
	return nil
}

func (n nativeLocalRuntime) Stop(ctx context.Context) error {
	if n.runtime == nil {
		return errors.New("runtime is closed")
	}
	_, apiErr := n.runtime.StopOwned(ctx, ctx)
	if apiErr != nil {
		return apiErr
	}
	return nil
}

var _ localcores.Runtime = nativeLocalRuntime{}
