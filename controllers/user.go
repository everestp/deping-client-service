package controllers

import (
	"encoding/json"
	"net/http"

	"github.com/everestp/deping-client-service/dto"
	"github.com/everestp/deping-client-service/services"
)

type UserController struct {
	userService services.UserService
}

func NewUserController(us services.UserService) *UserController {
	return &UserController{userService: us}
}

// POST /api/auth/register
func (uc *UserController) Register(w http.ResponseWriter, r *http.Request) {
	var req dto.RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body"})
		return
	}

	resp, err := uc.userService.Register(r.Context(), req)
	if err != nil {
		switch err {
		case services.ErrEmailTaken:
			writeJSON(w, http.StatusConflict, dto.ErrorResponse{Error: "email already registered"})
		case services.ErrWalletTaken:
			writeJSON(w, http.StatusConflict, dto.ErrorResponse{Error: "wallet already registered"})
		default:
			writeJSON(w, http.StatusInternalServerError, dto.ErrorResponse{Error: "registration failed"})
		}
		return
	}

	writeJSON(w, http.StatusCreated, resp)
}

// POST /api/auth/login
func (uc *UserController) Login(w http.ResponseWriter, r *http.Request) {
	var req dto.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, dto.ErrorResponse{Error: "invalid request body"})
		return
	}

	resp, err := uc.userService.Login(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, dto.ErrorResponse{Error: "invalid credentials"})
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// GET /api/auth/me
func (uc *UserController) Me(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, dto.ErrorResponse{Error: "unauthorized"})
		return
	}

	info, err := uc.userService.GetUserInfo(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, dto.ErrorResponse{Error: "user not found"})
		return
	}

	writeJSON(w, http.StatusOK, info)
}
