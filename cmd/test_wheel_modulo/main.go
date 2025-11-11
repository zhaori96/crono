package main

import "fmt"

// Simulação da wheel usando % (módulo) consistentemente
type SimpleWheel struct {
	slotCount uint32
	position  uint32
}

func (w *SimpleWheel) advancePosition() uint32 {
	w.position = (w.position + 1) % w.slotCount
	return w.position
}

func (w *SimpleWheel) calculateSlot(offset uint32) uint32 {
	return (w.position + offset) % w.slotCount
}

func main() {
	fmt.Println("=== Wheel com 50 slots usando % ===\n")

	wheel := &SimpleWheel{
		slotCount: 50,
		position:  0,
	}

	// Teste 1: Agendar 10 itens com offset 5
	fmt.Println("Teste 1: Agendando 10 itens com offset=5")
	fmt.Println("(todos deveriam ir para slots diferentes)\n")

	for i := 0; i < 10; i++ {
		slot := wheel.calculateSlot(5)
		fmt.Printf("Position %2d + offset 5 = slot %2d\n", wheel.position, slot)
		wheel.advancePosition()
	}

	// Teste 2: Avançar posição dando volta completa
	fmt.Println("\nTeste 2: Avançando 55 ticks (> 50 slots)")
	fmt.Println("Deveria dar mais de uma volta e voltar ao slot 5\n")

	wheel.position = 0
	fmt.Printf("Início: position = %d\n", wheel.position)

	for i := 0; i < 55; i++ {
		wheel.advancePosition()
	}

	fmt.Printf("Após 55 ticks: position = %d (esperado: 5)\n", wheel.position)

	if wheel.position == 5 {
		fmt.Println("✓ Correto!")
	} else {
		fmt.Println("✗ Erro!")
	}

	// Teste 3: Calcular offset para timeout de 550ms
	fmt.Println("\nTeste 3: Timeout de 550ms com tick=10ms")
	tickCount := uint64(55) // 550ms / 10ms
	roundsRequired := uint32(tickCount / uint64(wheel.slotCount))
	offset := uint32(tickCount % uint64(wheel.slotCount))

	fmt.Printf("tickCount = %d\n", tickCount)
	fmt.Printf("rounds = %d (550ms = %d rodadas completas + %d ticks)\n",
		roundsRequired, roundsRequired, offset)
	fmt.Printf("offset = %d\n", offset)

	wheel.position = 0
	targetSlot := wheel.calculateSlot(offset)
	fmt.Printf("Position %d + offset %d = slot %d\n", wheel.position, offset, targetSlot)

	// Teste 4: Comparar com 64 slots (potência de 2)
	fmt.Println("\n=== Comparação: 50 vs 64 slots ===\n")

	wheel50 := &SimpleWheel{slotCount: 50, position: 0}
	wheel64 := &SimpleWheel{slotCount: 64, position: 0}

	fmt.Println("Agendando com timeout de 127 ticks:")

	tickCount = 127
	rounds50 := tickCount / uint64(wheel50.slotCount)
	offset50 := tickCount % uint64(wheel50.slotCount)

	rounds64 := tickCount / uint64(wheel64.slotCount)
	offset64 := tickCount % uint64(wheel64.slotCount)

	fmt.Printf("\nCom 50 slots:\n")
	fmt.Printf("  rounds = %d, offset = %d\n", rounds50, offset50)
	fmt.Printf("  slot destino = %d\n", wheel50.calculateSlot(uint32(offset50)))

	fmt.Printf("\nCom 64 slots:\n")
	fmt.Printf("  rounds = %d, offset = %d\n", rounds64, offset64)
	fmt.Printf("  slot destino = %d\n", wheel64.calculateSlot(uint32(offset64)))

	fmt.Println("\n✓ Ambos funcionam perfeitamente!")
}
