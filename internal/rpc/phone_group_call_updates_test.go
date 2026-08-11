package rpc

import (
	"context"
	"testing"

	"github.com/iamxvbaba/td/clock"
	"github.com/iamxvbaba/td/tg"

	"telesrv/internal/domain"
)

func TestGroupCallUpdateContainerPreservesCachedChannelRights(t *testing.T) {
	const viewerUserID int64 = 1001

	r := &Router{clock: clock.System}
	updates := r.groupCallUpdateContainer(context.Background(), viewerUserID, domain.Channel{
		ID:                 2001,
		CreatorUserID:      viewerUserID,
		Broadcast:          true,
		ActiveCallID:       3001,
		ActiveCallNotEmpty: true,
	}, &tg.UpdateChannel{ChannelID: 2001}, nil)

	if len(updates.Chats) != 1 {
		t.Fatalf("chats = %d, want 1", len(updates.Chats))
	}
	channel, ok := updates.Chats[0].(*tg.Channel)
	if !ok {
		t.Fatalf("chat type = %T, want *tg.Channel", updates.Chats[0])
	}
	if !channel.Min {
		t.Fatal("group-call companion channel must be min to preserve cached admin rights")
	}
	if channel.Creator {
		t.Fatal("min channel must not replace the client's cached creator state")
	}
	if !channel.CallActive || !channel.CallNotEmpty {
		t.Fatalf("call flags = active:%v not_empty:%v, want both true", channel.CallActive, channel.CallNotEmpty)
	}
}
