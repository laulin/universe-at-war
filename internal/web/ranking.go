package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appranking "universeatwar/internal/app/ranking"
)

const rankingPageSize = 100

type rankingCategoryView struct {
	ID       appranking.Category
	Name     string
	Selected bool
}

type rankingPageData struct {
	pageShell
	Entries      []appranking.Entry
	Categories   []rankingCategoryView
	Category     appranking.Category
	CategoryName string
	Players      int
	Page         int
	Pages        int
	RangeStart   int
	RangeEnd     int
	PreviousLink string
	NextLink     string
	Own          *appranking.Entry
}

func (h *Handler) rankingPage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if principal.MustChangePassword {
		http.Redirect(response, request, "/password/change", http.StatusSeeOther)
		return
	}
	if h.ranking == nil {
		http.NotFound(response, request)
		return
	}
	category := rankingCategory(request.URL.Query().Get("category"))
	entries, err := h.ranking.List(request.Context(), principal, category)
	if errors.Is(err, appranking.ErrForbidden) {
		http.NotFound(response, request)
		return
	}
	if err != nil {
		http.Error(response, "ranking unavailable", http.StatusInternalServerError)
		return
	}
	planets := h.rankingBodies(request, principal)
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}

	page := positivePage(request.URL.Query().Get("page"))
	pages := (len(entries) + rankingPageSize - 1) / rankingPageSize
	if pages < 1 {
		pages = 1
	}
	if page > pages {
		page = pages
	}
	start := (page - 1) * rankingPageSize
	end := min(start+rankingPageSize, len(entries))
	data := rankingPageData{
		pageShell: h.gameShell(request.Context(), token, principal, "ranking", planets, h.rememberedBody(request)),
		Entries:   entries[start:end], Category: category, CategoryName: rankingCategoryName(category),
		Categories: rankingCategories(category), Players: len(entries), Page: page, Pages: pages,
	}
	if len(entries) > 0 {
		data.RangeStart, data.RangeEnd = start+1, end
	}
	if page > 1 {
		data.PreviousLink = rankingLink(category, page-1)
	}
	if page < pages {
		data.NextLink = rankingLink(category, page+1)
	}
	for index := range entries {
		if entries[index].Own {
			own := entries[index]
			data.Own = &own
			break
		}
	}
	h.render(response, http.StatusOK, "ranking", data)
}

func (h *Handler) rankingBodies(request *http.Request, principal appauth.Principal) []appeconomy.Planet {
	if h.economy == nil {
		return nil
	}
	planets, err := h.economy.Planets(request.Context(), principal)
	if err != nil {
		return nil
	}
	return planets
}

func rankingCategory(raw string) appranking.Category {
	category := appranking.Category(raw)
	switch category {
	case appranking.Economy, appranking.Research, appranking.Military:
		return category
	default:
		return appranking.Total
	}
}

func rankingCategoryName(category appranking.Category) string {
	switch category {
	case appranking.Economy:
		return "Économie"
	case appranking.Research:
		return "Recherche"
	case appranking.Military:
		return "Militaire"
	default:
		return "Général"
	}
}

func rankingCategories(selected appranking.Category) []rankingCategoryView {
	categories := []rankingCategoryView{
		{ID: appranking.Total, Name: "Général"},
		{ID: appranking.Economy, Name: "Économie"},
		{ID: appranking.Research, Name: "Recherche"},
		{ID: appranking.Military, Name: "Militaire"},
	}
	for index := range categories {
		categories[index].Selected = categories[index].ID == selected
	}
	return categories
}

func positivePage(raw string) int {
	page, err := strconv.Atoi(raw)
	if err != nil || page < 1 {
		return 1
	}
	return page
}

func rankingLink(category appranking.Category, page int) string {
	return fmt.Sprintf("/ranking?category=%s&page=%d", category, page)
}
