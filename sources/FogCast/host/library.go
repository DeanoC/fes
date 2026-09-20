package host

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/DeanoC/FogCast/protocol"
	"github.com/DeanoC/FogCast/targetclient"
)

type Library struct {
	games  []Game
	byID   map[string]Game
	client *targetclient.Client
}

func Open(configPath string, httpClient *http.Client) (*Library, error) {
	connection, err := LoadConnection(configPath)
	if err != nil {
		return nil, err
	}
	manifest, err := LoadManifest(connection.ManifestPath)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: connection.RequestTimeout}
	}
	games := slices.Clone(manifest.Games)
	byID := make(map[string]Game, len(games))
	for _, game := range games {
		byID[game.ID] = game
	}
	return &Library{games: games, byID: byID, client: targetclient.NewClient(connection.BaseURL, connection.Token, httpClient)}, nil
}

func (l *Library) Games() []Game {
	return slices.Clone(l.games)
}

func (l *Library) Health(ctx context.Context) (protocol.Health, error) {
	return l.client.Health(ctx)
}

func (l *Library) Status(ctx context.Context) (protocol.Status, error) {
	return l.client.Status(ctx)
}

func (l *Library) Launch(ctx context.Context, gameID string) (protocol.Status, error) {
	game, ok := l.byID[gameID]
	if !ok {
		return protocol.Status{}, fmt.Errorf("unknown game ID %q", gameID)
	}
	return l.client.Launch(ctx, protocol.LaunchRequest{GameID: game.ID, System: game.System, ROMPath: game.ROMPath})
}

func (l *Library) Stop(ctx context.Context) (protocol.Status, error) {
	return l.client.Stop(ctx)
}
