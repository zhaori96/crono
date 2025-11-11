package main

import (
	"context"
	"fmt"
	"time"

	"github.com/zhaori96/crono/internal/wheel"
)

type testTarget struct {
	id      int
	expired bool
}

func (t *testTarget) Expire() {
	t.expired = true
	fmt.Printf("Item %d expirado\n", t.id)
}

func (t *testTarget) Expired() bool {
	return t.expired
}

func (t *testTarget) CanExpire() bool {
	return true
}

func main() {
	fmt.Println("=== Testando Wheel com Diferentes Slot Counts ===\n")

	testCases := []uint32{1, 7, 50, 64, 100, 127}

	for _, slotCount := range testCases {
		fmt.Printf("Testando com %d slots...\n", slotCount)

		w, err := wheel.NewWheel(
			wheel.WithTickInterval(10*time.Millisecond),
			wheel.WithSlotCount(slotCount),
		)
		if err != nil {
			fmt.Printf("  ✗ Erro ao criar: %v\n\n", err)
			continue
		}

		if err := w.Start(); err != nil {
			fmt.Printf("  ✗ Erro ao iniciar: %v\n\n", err)
			continue
		}

		// Agenda 5 itens
		targets := make([]*testTarget, 5)
		for i := 0; i < 5; i++ {
			targets[i] = &testTarget{id: i}
			_, err := w.Schedule(targets[i], 50*time.Millisecond)
			if err != nil {
				fmt.Printf("  ✗ Erro ao agendar item %d: %v\n", i, err)
			}
		}

		// Espera expirar
		time.Sleep(100 * time.Millisecond)

		// Verifica se todos expiraram
		allExpired := true
		for _, t := range targets {
			if !t.expired {
				allExpired = false
				break
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		w.Stop(ctx)
		cancel()

		if allExpired {
			fmt.Printf("  ✓ Todos os itens expiraram corretamente\n")
		} else {
			fmt.Printf("  ✗ Alguns itens não expiraram\n")
		}

		fmt.Printf("  SlotCount retornado: %d\n\n", w.SlotCount())
	}
}
