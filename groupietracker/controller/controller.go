package controller

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	scoreBatURL = "https://www.scorebat.com/video-api/v3/"
	pageSize    = 9
	cacheTTL    = 2 * time.Minute
	cookieName  = "favorites"
)

type apiResponse struct {
	Response []Match `json:"response"`
}

type Match struct {
	Title        string `json:"title"`
	Competition  string `json:"competition"`
	MatchviewURL string `json:"matchviewUrl"`
	Thumbnail    string `json:"thumbnail"`
	DateRaw      string `json:"date"`
	Videos       []struct {
		Title string `json:"title"`
		Embed string `json:"embed"`
	} `json:"videos"`

	PrettyDate string        `json:"-"`
	EmbedHTML  template.HTML `json:"-"`
	IsFavorite bool          `json:"-"`
	Category   string        `json:"-"`
}

type listPageData struct {
	Title        string
	Matches      []Match
	Query        string
	Category     string
	CategoryName string
	CurrentPage  int
	TotalPages   int
	PrevPage     int
	NextPage     int
}

type detailPageData struct {
	Match
}

var (
	cacheMu     sync.Mutex
	cachedAt    time.Time
	cachedMatch []Match
	httpClient  = &http.Client{Timeout: 10 * time.Second}
)

func HomeHandler(w http.ResponseWriter, r *http.Request) {
	matches, err := loadMatchesWithFavorites(r)
	if err != nil {
		serverError(w, err)
		return
	}

	if len(matches) > 6 {
		matches = matches[:6]
	}

	data := listPageData{
		Title:   "Dernières vidéos",
		Matches: matches,
	}
	render(w, "home.html", data)
}

func CollectionHandler(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	page := parsePage(r.URL.Query().Get("page"))

	matches, err := loadMatchesWithFavorites(r)
	if err != nil {
		serverError(w, err)
		return
	}

	filtered := filterMatches(matches, query, category)
	paged, totalPages, prev, next := paginate(filtered, page, pageSize)

	title := "Toutes les ressources"
	if query != "" {
		title = "Résultats pour \"" + query + "\""
	} else if category != "" {
		title = "Catégorie : " + categoryLabel(category)
	}

	data := listPageData{
		Title:        title,
		Matches:      paged,
		Query:        query,
		Category:     category,
		CategoryName: categoryLabel(category),
		CurrentPage:  page,
		TotalPages:   totalPages,
		PrevPage:     prev,
		NextPage:     next,
	}
	render(w, "collection.html", data)
}

func CategoryHandler(w http.ResponseWriter, r *http.Request) {
	category := strings.TrimSpace(r.URL.Query().Get("category"))
	if category == "" {
		http.Redirect(w, r, "/matches", http.StatusSeeOther)
		return
	}
	r.URL.RawQuery = url.Values{
		"category": {category},
	}.Encode()
	CollectionHandler(w, r)
}

func SearchResultsHandler(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		http.Redirect(w, r, "/matches", http.StatusSeeOther)
		return
	}
	r.URL.RawQuery = url.Values{
		"q":    {query},
		"page": {r.URL.Query().Get("page")},
	}.Encode()
	CollectionHandler(w, r)
}

func FavoritesPageHandler(w http.ResponseWriter, r *http.Request) {
	matches, err := loadMatchesWithFavorites(r)
	if err != nil {
		serverError(w, err)
		return
	}

	favMatches := make([]Match, 0, len(matches))
	for _, m := range matches {
		if m.IsFavorite {
			favMatches = append(favMatches, m)
		}
	}

	data := listPageData{
		Title:   "Mes Favoris",
		Matches: favMatches,
	}
	render(w, "favorites.html", data)
}

func DetailHandler(w http.ResponseWriter, r *http.Request) {
	titleRaw := r.URL.Query().Get("title")
	if titleRaw == "" {
		http.Error(w, "title manquant", http.StatusBadRequest)
		return
	}

	// Décoder l'URL correctement
	title, err := url.QueryUnescape(titleRaw)
	if err != nil {
		title = strings.TrimSpace(titleRaw)
	} else {
		title = strings.TrimSpace(title)
	}

	matches, err := loadMatchesWithFavorites(r)
	if err != nil {
		serverError(w, err)
		return
	}

	// Recherche avec comparaison flexible (insensible à la casse et aux espaces)
	var foundMatch *Match
	for i := range matches {
		m := &matches[i]
		// Comparaison exacte d'abord
		if m.Title == title {
			foundMatch = m
			break
		}
		// Fallback: comparaison insensible à la casse et aux espaces multiples
		if strings.EqualFold(strings.Join(strings.Fields(m.Title), " "), strings.Join(strings.Fields(title), " ")) {
			foundMatch = m
			break
		}
	}

	if foundMatch == nil {
		log.Printf("Match non trouvé pour titre: %q (raw: %q)", title, titleRaw)
		http.NotFound(w, r)
		return
	}

	// Préparer l'embed HTML
	if len(foundMatch.Videos) > 0 {
		foundMatch.EmbedHTML = template.HTML(foundMatch.Videos[0].Embed)
	} else {
		log.Printf("Aucune vidéo trouvée pour: %q", foundMatch.Title)
		foundMatch.EmbedHTML = template.HTML(`<p style="padding: 20px; text-align: center; color: #666;">Aucune vidéo disponible pour ce match.</p>`)
	}

	render(w, "detail.html", detailPageData{Match: *foundMatch})
}

func ToggleFavoriteHandler(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.URL.Query().Get("title"))
	if title == "" {
		http.Error(w, "title manquant", http.StatusBadRequest)
		return
	}

	favs := getFavorites(r)
	if favs[title] {
		delete(favs, title)
	} else {
		favs[title] = true
	}
	saveFavorites(w, favs)

	redirect := r.URL.Query().Get("redirect")
	if redirect == "" {
		redirect = "/favorites"
	}
	http.Redirect(w, r, redirect, http.StatusSeeOther)
}

func AboutHandler(w http.ResponseWriter, r *http.Request) {
	data := struct{ Title string }{Title: "À propos"}
	render(w, "about.html", data)
}

func loadMatchesWithFavorites(r *http.Request) ([]Match, error) {
	matches, err := fetchMatches()
	if err != nil {
		return nil, err
	}

	favs := getFavorites(r)
	for i := range matches {
		matches[i].IsFavorite = favs[matches[i].Title]
	}
	return matches, nil
}

func fetchMatches() ([]Match, error) {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	if time.Since(cachedAt) < cacheTTL && len(cachedMatch) > 0 {
		return cloneMatches(cachedMatch), nil
	}

	resp, err := httpClient.Get(scoreBatURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	normalizeMatches(apiResp.Response)
	cachedMatch = apiResp.Response
	cachedAt = time.Now()

	if len(cachedMatch) > 0 {
		log.Printf("Chargé %d matchs depuis l'API ScoreBat", len(cachedMatch))
		if len(cachedMatch[0].Videos) > 0 {
			log.Printf("Exemple de vidéo pour '%s': %d vidéo(s) disponible(s)", cachedMatch[0].Title, len(cachedMatch[0].Videos))
		}
	}

	return cloneMatches(cachedMatch), nil
}

func normalizeMatches(matches []Match) {
	for i := range matches {
		m := &matches[i]

		if t, err := time.Parse(time.RFC3339, m.DateRaw); err == nil {
			m.PrettyDate = t.Format("02 Jan 2006 15:04")
		} else {
			m.PrettyDate = m.DateRaw
		}

		parts := strings.SplitN(m.Competition, ":", 2)
		if len(parts) > 0 {
			m.Category = strings.TrimSpace(strings.ToLower(parts[0]))
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].DateRaw > matches[j].DateRaw
	})
}

func cloneMatches(src []Match) []Match {
	out := make([]Match, len(src))
	copy(out, src)
	return out
}

func filterMatches(matches []Match, query, category string) []Match {
	if query == "" && category == "" {
		return matches
	}
	q := strings.ToLower(query)
	c := strings.ToLower(category)

	var filtered []Match
	for _, m := range matches {
		if q != "" {
			if !strings.Contains(strings.ToLower(m.Title), q) &&
				!strings.Contains(strings.ToLower(m.Competition), q) {
				continue
			}
		}
		if c != "" && m.Category != "" && !strings.Contains(m.Category, c) {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

func paginate(matches []Match, page, size int) (paged []Match, totalPages, prev, next int) {
	if size <= 0 {
		size = pageSize
	}
	totalPages = (len(matches) + size - 1) / size
	if totalPages == 0 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * size
	end := start + size
	if start > len(matches) {
		start = len(matches)
	}
	if end > len(matches) {
		end = len(matches)
	}
	paged = matches[start:end]
	if page > 1 {
		prev = page - 1
	}
	if page < totalPages {
		next = page + 1
	}
	return
}

func parsePage(raw string) int {
	if raw == "" {
		return 1
	}
	p, err := strconv.Atoi(raw)
	if err != nil || p < 1 {
		return 1
	}
	return p
}

func getFavorites(r *http.Request) map[string]bool {
	favs := make(map[string]bool)
	c, err := r.Cookie(cookieName)
	if err != nil {
		return favs
	}
	for _, title := range strings.Split(c.Value, "|") {
		title = strings.TrimSpace(title)
		if title != "" {
			favs[title] = true
		}
	}
	return favs
}

func saveFavorites(w http.ResponseWriter, favs map[string]bool) {
	titles := make([]string, 0, len(favs))
	for title := range favs {
		titles = append(titles, title)
	}
	sort.Strings(titles)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    strings.Join(titles, "|"),
		Path:     "/",
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
		HttpOnly: true,
	})
}

func render(w http.ResponseWriter, contentTemplate string, data interface{}) {
	tmpl, err := template.ParseFiles(
		"template/accueil.html",
		"template/"+contentTemplate,
	)
	if err != nil {
		serverError(w, err)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		serverError(w, err)
	}
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("server error: %v", err)
	http.Error(w, "Une erreur est survenue", http.StatusInternalServerError)
}

func categoryLabel(raw string) string {
	if raw == "" {
		return ""
	}
	return strings.Title(strings.ToLower(raw))
}
