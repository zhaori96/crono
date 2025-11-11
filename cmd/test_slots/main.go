package main

import "fmt"

func main() {
	// Teste com 50 slots (não é potência de 2)
	slotCount := uint32(50)
	slotMask := slotCount - 1 // 49

	fmt.Println("=== Teste com 50 slots (não é potência de 2) ===")
	fmt.Printf("slotCount = %d, slotMask = %d\n\n", slotCount, slotMask)

	testCases := []struct {
		position uint32
		offset   uint32
	}{
		{0, 25},
		{10, 45},
		{20, 20},
		{30, 30},
		{40, 15},
		{0, 70},
		{25, 40},
	}

	for _, tc := range testCases {
		sum := tc.position + tc.offset
		expectedModulo := sum % slotCount
		resultMask := sum & slotMask

		status := "✓"
		if expectedModulo != resultMask {
			status = "✗ BUG!"
		}

		fmt.Printf("(%2d + %2d) = %3d | ", tc.position, tc.offset, sum)
		fmt.Printf("Esperado (%%): %2d | ", expectedModulo)
		fmt.Printf("Resultado (&): %2d | ", resultMask)
		fmt.Printf("%s\n", status)
	}

	// Agora com 64 slots (potência de 2)
	fmt.Println("\n=== Teste com 64 slots (potência de 2) ===")
	slotCount = 64
	slotMask = slotCount - 1 // 63

	fmt.Printf("slotCount = %d, slotMask = %d\n\n", slotCount, slotMask)

	for _, tc := range testCases {
		sum := tc.position + tc.offset
		expectedModulo := sum % slotCount
		resultMask := sum & slotMask

		status := "✓"
		if expectedModulo != resultMask {
			status = "✗ BUG!"
		}

		fmt.Printf("(%2d + %2d) = %3d | ", tc.position, tc.offset, sum)
		fmt.Printf("Esperado (%%): %2d | ", expectedModulo)
		fmt.Printf("Resultado (&): %2d | ", resultMask)
		fmt.Printf("%s\n", status)
	}
}
