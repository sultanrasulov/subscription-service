// Package application holds the use cases of the subscription context: one
// service with one method per use case.
//
// It receives value objects and returns domain objects; reading raw input is
// the HTTP adapter's job. It returns errors wrapped with the use case's name
// and never logs them, so each is logged once, where the request ends. It logs
// only the business events: a subscription created, updated or deleted.
package application

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/sultanrasulov/subscription-service/internal/subscription/domain"
)

// Service runs the subscription use cases on a repository.
type Service struct {
	repo domain.Repository
	log  *slog.Logger
}

// NewService returns a service that stores subscriptions in repo and logs its
// events to log.
func NewService(repo domain.Repository, log *slog.Logger) *Service {
	return &Service{repo: repo, log: log}
}

// Create stores a new subscription with a fresh ID and returns it.
//
// The ID is a UUID version 7, which starts with its creation time, so IDs
// sort roughly by creation and pages ordered by ID stay stable.
func (s *Service) Create(ctx context.Context, userID domain.UserID, name domain.ServiceName, price domain.Price, period domain.SubscriptionPeriod) (domain.Subscription, error) {
	raw, err := uuid.NewV7()
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("create subscription: generate an ID: %w", err)
	}
	id, err := domain.NewSubscriptionID(raw)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("create subscription: %w", err)
	}
	sub, err := domain.NewSubscription(id, userID, name, price, period)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("create subscription: %w", err)
	}
	if err := s.repo.Create(ctx, sub); err != nil {
		return domain.Subscription{}, fmt.Errorf("create subscription: %w", err)
	}
	s.event(ctx, "subscription created", sub)
	return sub, nil
}

// Get returns the subscription with the given ID.
func (s *Service) Get(ctx context.Context, id domain.SubscriptionID) (domain.Subscription, error) {
	sub, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("get subscription: %w", err)
	}
	return sub, nil
}

// Update replaces the service name, the price and the period of the
// subscription with the given ID, and returns it. Two concurrent updates do
// not conflict: the last one written wins.
func (s *Service) Update(ctx context.Context, id domain.SubscriptionID, name domain.ServiceName, price domain.Price, period domain.SubscriptionPeriod) (domain.Subscription, error) {
	sub, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.Subscription{}, fmt.Errorf("update subscription: %w", err)
	}
	if err := sub.Update(name, price, period); err != nil {
		return domain.Subscription{}, fmt.Errorf("update subscription: %w", err)
	}
	if err := s.repo.Update(ctx, sub); err != nil {
		return domain.Subscription{}, fmt.Errorf("update subscription: %w", err)
	}
	s.event(ctx, "subscription updated", sub)
	return sub, nil
}

// Delete removes the subscription with the given ID.
func (s *Service) Delete(ctx context.Context, id domain.SubscriptionID) error {
	sub, err := s.repo.Delete(ctx, id)
	if err != nil {
		return fmt.Errorf("delete subscription: %w", err)
	}
	s.event(ctx, "subscription deleted", sub)
	return nil
}

// List returns the page of subscriptions that the query selects, and the
// number of all subscriptions that match its filter.
func (s *Service) List(ctx context.Context, q domain.ListQuery) ([]domain.Subscription, int, error) {
	items, total, err := s.repo.List(ctx, q)
	if err != nil {
		return nil, 0, fmt.Errorf("list subscriptions: %w", err)
	}
	return items, total, nil
}

// event logs a business event with the IDs that tell what it happened to.
func (s *Service) event(ctx context.Context, msg string, sub domain.Subscription) {
	s.log.InfoContext(ctx, msg,
		"subscription_id", sub.ID().UUID().String(),
		"user_id", sub.UserID().UUID().String(),
	)
}
