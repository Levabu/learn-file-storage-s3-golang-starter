package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/bootdotdev/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
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

	videoMeta, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to get video metadata", err)
		return
	}

	if videoMeta.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Unauthorized to access this video", err)
		return
	}

	const maxMemory = 1 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxMemory)
	file, header, err := r.FormFile("video")
	defer file.Close()
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}

	contentType := header.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Bad file type", err)
		return
	}

	// save temp
	tempName := "tubely-upload.mp4"
	tempFile, err := os.CreateTemp("", tempName)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temp file", err)
		return
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()
	_, err = io.Copy(tempFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to write temp file", err)
		return
	}

	// save processed temp
	processedTempFilePath, err := processVideoForFastStart(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create processed temp file", err)
		return
	}
	processedTempFile, err := os.Open(processedTempFilePath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to open processed temp file", err)
		return
	}
	defer os.Remove(processedTempFile.Name())
	defer processedTempFile.Close()

	// get prefix
	aspectRatio, err := getVideoAspectRatio(tempFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to get aspect ratio", err)
	}
	var prefix string
	switch aspectRatio {
	case "16:9":
		prefix = "landscape"
	case "9:16":
		prefix = "portrait"
	default:
		prefix = "other"
	}
	prefix += "/"
	
	// upload to s3
	randBytes := make([]byte, 32)
	rand.Read(randBytes)
	fileKey := prefix + base64.RawURLEncoding.EncodeToString(randBytes) + ".mp4"

	_, err = cfg.s3Client.PutObject(r.Context(), &s3.PutObjectInput{
		Bucket: aws.String(cfg.s3Bucket),
		Key: aws.String(fileKey),
		Body: processedTempFile,
		ContentType: aws.String(mediaType),
	})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to upload file", err)
		return
	}

	// newVideoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, fileKey)
	newVideoURL := fmt.Sprintf("%s,%s", cfg.s3Bucket, fileKey)
	videoMeta.VideoURL = &newVideoURL
	err = cfg.db.UpdateVideo(videoMeta)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable updating video metadata", err)
		return
	}

	signedVideo, err := cfg.dbVideoToSignedVideo(videoMeta)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to generate signed video metadata", err)
		return
	}

	respondWithJSON(w, http.StatusOK, signedVideo)
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	client := s3.NewPresignClient(s3Client)
	r, err := client.PresignGetObject(
		context.Background(), 
		&s3.GetObjectInput{
			Bucket: &bucket,
			Key: &key,
		},
		s3.WithPresignExpires(expireTime),
	)
	if err != nil {
		return "", err
	}
	return r.URL, nil
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, errors.New("video URL is nil")
	}
	s := strings.Split(*video.VideoURL, ",")
	if len(s) != 2 {
		return database.Video{}, errors.New("wrong format for vidoe url: should be 'bucket,key'")
	}
	bucket := s[0]
	key := s[1]
	url, err := generatePresignedURL(cfg.s3Client, bucket, key, 15 * time.Minute)
	if err != nil {
		return database.Video{}, fmt.Errorf("error generating presigned url: %s", err)
	}
	video.VideoURL = &url
	return video, nil
}