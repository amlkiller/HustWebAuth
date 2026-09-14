package cmd

import (
	"context"
	"log"

	"github.com/kardianos/service"
)

func (p *program) Start(service.Service) error {
	// Start should not block. Do the actual work async.
	log.Println("Starting HustWebAuth service...")
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.run()
	}()
	return nil
}

func (p *program) run() {
	runCycleWithContext(p.ctx)
}

func (p *program) Stop(service.Service) error {
	log.Println("Stopping HustWebAuth service...")
	if p.cancel != nil {
		p.cancel()
	}
	p.wg.Wait()
	CloseIdleHTTPConnections()
	return nil
}
