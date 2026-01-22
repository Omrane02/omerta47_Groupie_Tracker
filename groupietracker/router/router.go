package router

import (
	"net/http"

	"groupietracker/controller"
)

func SetupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	
	fs := http.FileServer(http.Dir("static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	
	mux.HandleFunc("/", controller.HomeHandler)
	mux.HandleFunc("/matches", controller.CollectionHandler)        // Page Principale/Recherche/Catégorie
	mux.HandleFunc("/match", controller.DetailHandler)              // Page Détail
	mux.HandleFunc("/favorites", controller.FavoritesPageHandler)   // Page Favoris
	mux.HandleFunc("/fav-toggle", controller.ToggleFavoriteHandler) // Action de favoris
	mux.HandleFunc("/search", controller.SearchResultsHandler)      // Page Résultats
	mux.HandleFunc("/category", controller.CategoryHandler)         // Page Catégorie
	mux.HandleFunc("/about", controller.AboutHandler)               // Page À propos 

	return mux
}
