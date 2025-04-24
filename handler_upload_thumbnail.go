package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadThumbnail(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}


	fmt.Println("uploading thumbnail for video", videoID, "by user", userID)

	// TODO: implement the upload here
	const maxMemory = 10 << 20;
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("thumbnail")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	videoMeta, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get video metadata", err)
		return
	}

	if videoMeta.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized to access this video", err)
		return
	}

	contentType := header.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || !slices.Contains([]string{"image/jpeg", "image/png"}, mediaType) {
		respondWithError(w, http.StatusBadRequest, "Bad file type", err)
		return
	}

	// save
	fileExtension, found := strings.CutPrefix(mediaType, "image/")
	if !found {
		respondWithError(w, http.StatusBadRequest, "Baddd file type", err)
		return
	}

	randBytes := make([]byte, 32)
	rand.Read(randBytes)
	fileName := base64.RawURLEncoding.EncodeToString(randBytes)
	filePath := filepath.Join(cfg.assetsRoot, fileName) + "." + fileExtension

	newFile, err := os.Create(filePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error creating file", err)
		return
	}
	_, err = io.Copy(newFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error writing file", err)
		return
	}

	thumbnailURL := fmt.Sprintf("http://localhost:%v/assets/%v.%v", cfg.port, fileName, fileExtension)

	videoMeta.ThumbnailURL = &thumbnailURL

	err = cfg.db.UpdateVideo(videoMeta)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to update video metadata", err)
		return
	}

	respondWithJSON(w, http.StatusOK, videoMeta)
}
