package solarman

func (inv *InverterLogger) SignedToFloat(data uint16) float64 {
	// Вхідні дані: значення зчитане з реєстру
	value := int(data)
	maxInt := 0xFFFF // Максимальне значення 16-бітного unsigned

	// Перевірка на знак (signed/unsigned)
	if value > maxInt/2 {
		value = value - (maxInt + 1) // Перетворення на від'ємне
	}

	// Повертаємо фінальний результат
	return float64(value)
}
