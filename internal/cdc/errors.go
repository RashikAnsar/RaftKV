package cdc

import "errors"

var (
	// ErrPublisherClosed is returned when attempting to publish to a closed publisher
	ErrPublisherClosed = errors.New("cdc: publisher is closed")

	// ErrSubscriberClosed is returned when attempting to receive from a closed subscriber
	ErrSubscriberClosed = errors.New("cdc: subscriber is closed")

	// ErrSubscriberNotFound is returned when a subscriber ID is not found
	ErrSubscriberNotFound = errors.New("cdc: subscriber not found")

	// ErrBufferFull is returned when the subscriber buffer is full
	ErrBufferFull = errors.New("cdc: subscriber buffer is full")
)
