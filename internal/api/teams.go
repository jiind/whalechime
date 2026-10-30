package api

import (
	"net/http"

	"github.com/jiind/whalechime/internal/store"
)

// teams는 관리자가 사용자 팀을 조회하게 한다.
func (a *API) teams(w http.ResponseWriter, r *http.Request, _ store.User) {
	items, err := a.Store.ListTeams(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// createTeam은 이름을 검증하고 새 팀을 등록한다.
func (a *API) createTeam(w http.ResponseWriter, r *http.Request, _ store.User) {
	// 1. 팀 이름의 입력 계약을 확인한다.
	var input struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if !validName(input.Name) {
		writeError(w, http.StatusBadRequest, "팀 이름은 공백을 제외한 1~100자로 입력하세요")
		return
	}

	// 2. 관리자에게 새 팀의 식별자를 반환한다.
	team, err := a.Store.CreateTeam(r.Context(), input.Name)
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, team)
}

// deleteTeam은 팀과 팀을 통한 접근 권한을 함께 제거한다.
func (a *API) deleteTeam(w http.ResponseWriter, r *http.Request, _ store.User) {
	if err := a.Store.DeleteTeam(r.Context(), r.PathValue("team")); err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// teamMembers는 선택한 팀의 구성원을 반환한다.
func (a *API) teamMembers(w http.ResponseWriter, r *http.Request, _ store.User) {
	items, err := a.Store.ListTeamMembers(r.Context(), r.PathValue("team"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// serverTeams는 선택한 서버의 담당 팀을 반환한다.
func (a *API) serverTeams(w http.ResponseWriter, r *http.Request, _ store.User) {
	items, err := a.Store.ListServerTeams(r.Context(), r.PathValue("server"))
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// setTeamLink는 팀 구성원 또는 서버 담당 팀 연결을 변경한다.
func (a *API) setTeamLink(w http.ResponseWriter, r *http.Request, _ store.User) {
	// 1. 필수 활성화 값을 확인한다.
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if input.Enabled == nil {
		writeError(w, http.StatusBadRequest, "enabled 값이 필요합니다")
		return
	}

	// 2. 경로에 따라 팀 구성원 또는 서버 연결만 변경한다.
	var err error
	if r.PathValue("server") != "" {
		err = a.Store.SetServerTeam(r.Context(), r.PathValue("server"), r.PathValue("team"), *input.Enabled)
	} else {
		err = a.Store.SetTeamMember(r.Context(), r.PathValue("team"), r.PathValue("user"), *input.Enabled)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
