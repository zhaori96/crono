package main

import "fmt"

func main() {
	fmt.Println("=== Simulação: Wheel com 50 slots ===\n")

	slotCount := uint32(50)
	slotMask := slotCount - 1 // 49

	// Simular 10 agendamentos com timeout = 550ms
	// tickInterval = 10ms → 55 ticks
	tickCount := uint64(55)

	fmt.Println("Agendando 10 itens com 55 ticks de timeout:")
	fmt.Println("(todos deveriam ir pro mesmo slot)\n")

	for position := uint32(0); position < 10; position++ {
		offset := uint32(tickCount % uint64(slotCount)) // = 5

		// Cálculo CORRETO (módulo)
		correctSlot := (position + offset) % slotCount

		// Cálculo ERRADO (usado no código com máscara)
		wrongSlot := (position + offset) & slotMask

		status := "✓"
		if correctSlot != wrongSlot {
			status = "✗ BUG!"
		}

		fmt.Printf("Position %2d | offset %2d | ", position, offset)
		fmt.Printf("Correto: slot %2d | ", correctSlot)
		fmt.Printf("Errado: slot %2d | ", wrongSlot)
		fmt.Printf("%s\n", status)
	}

	fmt.Println("\n=== Simulação: Wheel avançando posição ===\n")

	position := uint32(0)
	fmt.Println("Avançando posição de 0 até dar a volta completa:")

	for i := 0; i < 55; i++ {
		nextCorrect := (position + 1) % slotCount
		nextWrong := (position + 1) & slotMask

		status := "✓"
		if nextCorrect != nextWrong {
			status = fmt.Sprintf("✗ PULA de %d pra %d (deveria ser %d)",
				position, nextWrong, nextCorrect)
		}

		if status != "✓" {
			fmt.Printf("Tick %2d: pos=%2d → %s\n", i, position, status)
		}

		position = nextCorrect // usa correto para continuar
	}
}
