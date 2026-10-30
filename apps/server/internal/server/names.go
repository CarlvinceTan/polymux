package server

import "math/rand/v2"

func randomName() string {
	names := [...]string{
		"Adam", "Aidan", "Alex", "Alice", "Alma", "Amir", "Anika", "Arlo",
		"Asher", "Avery", "Beau", "Blake", "Cora", "Daisy", "Dana", "Daria",
		"Eden", "Elena", "Elise", "Ella", "Ellis", "Emil", "Emma", "Enzo",
		"Erin", "Esme", "Ezra", "Felix", "Finn", "Flora", "Hugo", "Iris",
		"Ivan", "Jade", "Jamie", "Jesse", "Jude", "Julia", "Kian", "Kira",
		"Lara", "Leah", "Leon", "Levi", "Liam", "Lila", "Luca", "Lucy",
		"Luna", "Maia", "Mara", "Maya", "Mila", "Milo", "Mira", "Myra",
		"Nash", "Nico", "Nina", "Noah", "Nora", "Nova", "Omar", "Opal",
		"Owen", "Remi", "Remy", "Rhea", "Riley", "Robin", "Rosa", "Ruby",
		"Ryan", "Sage", "Sara", "Sean", "Skye", "Tara", "Theo", "Toby",
		"Vera", "Wren", "Yara", "Zane", "Zara", "Zora",
	}
	return names[rand.IntN(len(names))]
}
