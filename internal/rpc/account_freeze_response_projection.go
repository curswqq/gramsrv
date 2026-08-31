package rpc

import (
	"context"
	"reflect"

	"github.com/iamxvbaba/td/tg"
	"telesrv/internal/domain"
)

// accountFreezeBatchReader is implemented by admin.Service. Keeping the batch
// capability optional preserves the narrow AccountFreezeService contract used
// by lightweight tests while production pays one bounded query per response.
type accountFreezeBatchReader interface {
	AccountFreezes(ctx context.Context, userIDs []int64) (map[int64]domain.AccountFreeze, error)
}

// applyAuthoritativeAccountFreezesToResponse is the final response-boundary
// guard for frozen account presentation. Telegram clients treat every user
// object as an authoritative cache replacement, including user objects nested
// in supplemental profile responses. A single stale envelope without
// bot_verification_icon would therefore erase the snowflake installed by
// users.getFullUser.
//
// The durable freeze fact is checked after the typed handler has completed and
// immediately before exact-layer encoding. This makes every current and future
// response envelope project the same deleted-style tombstone without deleting
// or otherwise mutating the account itself.
func (r *Router) applyAuthoritativeAccountFreezesToResponse(ctx context.Context, viewerUserID int64, response any) error {
	if r == nil || r.deps.AccountFreeze == nil || viewerUserID == 0 || response == nil {
		return nil
	}
	return r.applyAuthoritativeAccountFreezesToResponses(ctx, []int64{viewerUserID}, []any{response})
}

// applyAuthoritativeAccountFreezesToResponses projects a batch of
// viewer-specific envelopes using one durable lookup. Outbox claims may contain
// many events and must not turn the final cache guard into one SQL query per
// event.
func (r *Router) applyAuthoritativeAccountFreezesToResponses(ctx context.Context, viewerUserIDs []int64, responses []any) error {
	if r == nil || r.deps.AccountFreeze == nil || len(viewerUserIDs) != len(responses) {
		return nil
	}

	usersByResponse := make([][]*tg.User, len(responses))
	ids := make([]int64, 0)
	seen := make(map[int64]struct{})
	for i, response := range responses {
		viewerUserID := viewerUserIDs[i]
		if viewerUserID == 0 || response == nil {
			continue
		}
		users := collectResponseUsers(response)
		usersByResponse[i] = users
		for _, user := range users {
			if user == nil || user.ID == 0 || user.ID == viewerUserID {
				continue
			}
			if _, ok := seen[user.ID]; ok {
				continue
			}
			seen[user.ID] = struct{}{}
			ids = append(ids, user.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}

	freezes := make(map[int64]domain.AccountFreeze)
	if batch, ok := r.deps.AccountFreeze.(accountFreezeBatchReader); ok {
		var err error
		freezes, err = batch.AccountFreezes(ctx, ids)
		if err != nil {
			return err
		}
	} else {
		for _, userID := range ids {
			freeze, found, err := r.deps.AccountFreeze.AccountFreeze(ctx, userID)
			if err != nil {
				return err
			}
			if found && freeze.Frozen {
				freezes[userID] = freeze
			}
		}
	}

	for i, users := range usersByResponse {
		viewerUserID := viewerUserIDs[i]
		for _, user := range users {
			if user == nil || user.ID == viewerUserID {
				continue
			}
			freeze, ok := freezes[user.ID]
			if !ok || !freeze.Frozen {
				continue
			}
			projected := tgDeletedStyleUser(domain.User{
				ID:                        user.ID,
				AccessHash:                user.AccessHash,
				Deleted:                   true,
				PublicFrozen:              true,
				FrozenBadgeIconDocumentID: freeze.BadgeIconDocumentID,
				RestrictionReasons:        domain.AccountFrozenRestrictionReasons(),
			})
			*user = *projected
		}
	}
	return nil
}

var tgUserPointerType = reflect.TypeOf((*tg.User)(nil))

// collectResponseUsers walks generated TL response values. Generated response
// graphs are acyclic in practice, but pointer tracking and a depth bound keep
// this guard safe if a future hand-written result embeds a cycle.
func collectResponseUsers(response any) []*tg.User {
	users := make([]*tg.User, 0)
	seenPointers := make(map[uintptr]struct{})
	var walk func(reflect.Value, int)
	walk = func(value reflect.Value, depth int) {
		if !value.IsValid() || depth > 64 {
			return
		}
		for value.Kind() == reflect.Interface {
			if value.IsNil() {
				return
			}
			value = value.Elem()
		}
		if value.Type() == tgUserPointerType {
			if !value.IsNil() && value.CanInterface() {
				users = append(users, value.Interface().(*tg.User))
			}
			return
		}
		switch value.Kind() {
		case reflect.Pointer:
			if value.IsNil() {
				return
			}
			pointer := value.Pointer()
			if pointer != 0 {
				if _, ok := seenPointers[pointer]; ok {
					return
				}
				seenPointers[pointer] = struct{}{}
			}
			walk(value.Elem(), depth+1)
		case reflect.Struct:
			for i := 0; i < value.NumField(); i++ {
				field := value.Field(i)
				if field.CanInterface() {
					walk(field, depth+1)
				}
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < value.Len(); i++ {
				walk(value.Index(i), depth+1)
			}
		case reflect.Map:
			iterator := value.MapRange()
			for iterator.Next() {
				walk(iterator.Value(), depth+1)
			}
		}
	}
	walk(reflect.ValueOf(response), 0)
	return users
}
