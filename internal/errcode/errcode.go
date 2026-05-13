package errcode

import "log"

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

const (
	CodeInternalError                      = "COMMON_INTERNAL_ERROR"
	CodeAuthUnauthorized                   = "AUTH_UNAUTHORIZED"
	CodeVideoFileRequired                  = "VIDEO_FILE_REQUIRED"
	CodeVideoUnsupportedFormat             = "VIDEO_UNSUPPORTED_FORMAT"
	CodeVideoUploadDirPrepareFailed        = "VIDEO_UPLOAD_DIR_PREPARE_FAILED"
	CodeVideoUploadSaveFailed              = "VIDEO_UPLOAD_SAVE_FAILED"
	CodeVideoJobCreateFailed               = "VIDEO_JOB_CREATE_FAILED"
	CodePlaylistProfileInvalid             = "PLAYLIST_PROFILE_INVALID"
	CodePlaylistNotFound                   = "PLAYLIST_NOT_FOUND"
	CodePlaylistProcessFailed              = "PLAYLIST_PROCESS_FAILED"
	CodeManifestProfileInvalid             = "MANIFEST_PROFILE_INVALID"
	CodeManifestNotFound                   = "MANIFEST_NOT_FOUND"
	CodeManifestProcessFailed              = "MANIFEST_PROCESS_FAILED"
	CodeVideoNotFound                      = "VIDEO_NOT_FOUND"
	CodeTokenRequired                      = "TOKEN_REQUIRED"
	CodeTokenInvalidOrExpired              = "TOKEN_INVALID_OR_EXPIRED"
	CodeTokenValidateFailed                = "TOKEN_VALIDATE_FAILED"
	CodeTokenExpiryParseFailed             = "TOKEN_EXPIRY_PARSE_FAILED"
	CodeDBTransactionBeginFailed           = "DB_TRANSACTION_BEGIN_FAILED"
	CodeTokenDeleteFailed                  = "TOKEN_DELETE_FAILED"
	CodeJobDeleteFailed                    = "JOB_DELETE_FAILED"
	CodeVideoDeleteFailed                  = "VIDEO_DELETE_FAILED"
	CodeDBTransactionCommitFailed          = "DB_TRANSACTION_COMMIT_FAILED"
	CodeVideoVisibilityRequired            = "VIDEO_VISIBILITY_REQUIRED"
	CodeVideoVisibilityInvalid             = "VIDEO_VISIBILITY_INVALID"
	CodeVideoVisibilityUpdateFailed        = "VIDEO_VISIBILITY_UPDATE_FAILED"
	CodeTokenGenerateFailed                = "TOKEN_GENERATE_FAILED"
	CodeTokenStoreFailed                   = "TOKEN_STORE_FAILED"
	CodeRefererNotAllowed                  = "REFERER_NOT_ALLOWED"
	CodeVideoReferrerWhitelistInvalid      = "VIDEO_REFERRER_WHITELIST_INVALID"
	CodeVideoReferrerWhitelistUpdateFailed = "VIDEO_REFERRER_WHITELIST_UPDATE_FAILED"
	CodeVideoNotReady                      = "VIDEO_NOT_READY"
	CodeVideoOriginalNotFound              = "VIDEO_ORIGINAL_NOT_FOUND"
	CodeStreamAssetNotFound                = "STREAM_ASSET_NOT_FOUND"
	CodeVideoQueryFailed                   = "VIDEO_QUERY_FAILED"
	CodeVideoScanFailed                    = "VIDEO_SCAN_FAILED"
	CodeVideoIterateFailed                 = "VIDEO_ITERATE_FAILED"
	CodeJobQueryFailed                     = "JOB_QUERY_FAILED"
	CodeJobScanFailed                      = "JOB_SCAN_FAILED"
	CodeJobIterateFailed                   = "JOB_ITERATE_FAILED"
	CodeThumbnailNotFound                  = "THUMBNAIL_NOT_FOUND"
	CodeJobProcessingFailed                = "JOB_PROCESSING_FAILED"
)

var (
	ErrInternalError = Error{Code: CodeInternalError, Message: "internal server error"}

	ErrAuthUnauthorized                   = Error{Code: CodeAuthUnauthorized, Message: "unauthorized"}
	ErrVideoFileRequired                  = Error{Code: CodeVideoFileRequired, Message: "file is required"}
	ErrVideoUnsupportedFormat             = Error{Code: CodeVideoUnsupportedFormat, Message: "only .mp4 is supported"}
	ErrVideoUploadDirPrepareFailed        = Error{Code: CodeVideoUploadDirPrepareFailed, Message: "failed to prepare upload directory"}
	ErrVideoUploadSaveFailed              = Error{Code: CodeVideoUploadSaveFailed, Message: "failed to save uploaded file"}
	ErrVideoJobCreateFailed               = Error{Code: CodeVideoJobCreateFailed, Message: "failed to create video job"}
	ErrPlaylistProfileInvalid             = Error{Code: CodePlaylistProfileInvalid, Message: "valid hls profile is required"}
	ErrPlaylistNotFound                   = Error{Code: CodePlaylistNotFound, Message: "playlist not found"}
	ErrPlaylistProcessFailed              = Error{Code: CodePlaylistProcessFailed, Message: "failed to process playlist"}
	ErrManifestProfileInvalid             = Error{Code: CodeManifestProfileInvalid, Message: "valid dash profile is required"}
	ErrManifestNotFound                   = Error{Code: CodeManifestNotFound, Message: "manifest not found"}
	ErrManifestProcessFailed              = Error{Code: CodeManifestProcessFailed, Message: "failed to process manifest"}
	ErrVideoNotFound                      = Error{Code: CodeVideoNotFound, Message: "video not found"}
	ErrTokenRequired                      = Error{Code: CodeTokenRequired, Message: "token is required"}
	ErrTokenInvalidOrExpired              = Error{Code: CodeTokenInvalidOrExpired, Message: "invalid or expired token"}
	ErrTokenValidateFailed                = Error{Code: CodeTokenValidateFailed, Message: "failed to validate token"}
	ErrTokenExpiryParseFailed             = Error{Code: CodeTokenExpiryParseFailed, Message: "failed to parse token expiry"}
	ErrDBTransactionBeginFailed           = Error{Code: CodeDBTransactionBeginFailed, Message: "failed to begin transaction"}
	ErrTokenDeleteFailed                  = Error{Code: CodeTokenDeleteFailed, Message: "failed to delete tokens"}
	ErrJobDeleteFailed                    = Error{Code: CodeJobDeleteFailed, Message: "failed to delete jobs"}
	ErrVideoDeleteFailed                  = Error{Code: CodeVideoDeleteFailed, Message: "failed to delete video"}
	ErrDBTransactionCommitFailed          = Error{Code: CodeDBTransactionCommitFailed, Message: "failed to commit transaction"}
	ErrVideoVisibilityRequired            = Error{Code: CodeVideoVisibilityRequired, Message: "visibility is required"}
	ErrVideoVisibilityInvalid             = Error{Code: CodeVideoVisibilityInvalid, Message: "visibility must be 'public' or 'private'"}
	ErrVideoVisibilityUpdateFailed        = Error{Code: CodeVideoVisibilityUpdateFailed, Message: "failed to update visibility"}
	ErrTokenGenerateFailed                = Error{Code: CodeTokenGenerateFailed, Message: "failed to generate token"}
	ErrTokenStoreFailed                   = Error{Code: CodeTokenStoreFailed, Message: "failed to store token"}
	ErrRefererNotAllowed                  = Error{Code: CodeRefererNotAllowed, Message: "referer is not allowed"}
	ErrVideoReferrerWhitelistInvalid      = Error{Code: CodeVideoReferrerWhitelistInvalid, Message: "referrer whitelist must contain domains only"}
	ErrVideoReferrerWhitelistUpdateFailed = Error{Code: CodeVideoReferrerWhitelistUpdateFailed, Message: "failed to update referrer whitelist"}
	ErrVideoNotReady                      = Error{Code: CodeVideoNotReady, Message: "video not found or not ready"}
	ErrVideoOriginalNotFound              = Error{Code: CodeVideoOriginalNotFound, Message: "original file not found"}
	ErrStreamAssetNotFound                = Error{Code: CodeStreamAssetNotFound, Message: "asset not found"}
	ErrVideoQueryFailed                   = Error{Code: CodeVideoQueryFailed, Message: "failed to query videos"}
	ErrVideoScanFailed                    = Error{Code: CodeVideoScanFailed, Message: "failed to scan video"}
	ErrVideoIterateFailed                 = Error{Code: CodeVideoIterateFailed, Message: "failed to iterate videos"}
	ErrJobQueryFailed                     = Error{Code: CodeJobQueryFailed, Message: "failed to query jobs"}
	ErrJobScanFailed                      = Error{Code: CodeJobScanFailed, Message: "failed to scan job"}
	ErrJobIterateFailed                   = Error{Code: CodeJobIterateFailed, Message: "failed to iterate jobs"}
	ErrThumbnailNotFound                  = Error{Code: CodeThumbnailNotFound, Message: "thumbnail not found"}

	ErrJobProcessingFailed = Error{Code: CodeJobProcessingFailed, Message: "video processing failed"}
)

var catalog = []Error{
	ErrInternalError,
	ErrAuthUnauthorized,
	ErrVideoFileRequired,
	ErrVideoUnsupportedFormat,
	ErrVideoUploadDirPrepareFailed,
	ErrVideoUploadSaveFailed,
	ErrVideoJobCreateFailed,
	ErrPlaylistProfileInvalid,
	ErrPlaylistNotFound,
	ErrPlaylistProcessFailed,
	ErrManifestProfileInvalid,
	ErrManifestNotFound,
	ErrManifestProcessFailed,
	ErrVideoNotFound,
	ErrTokenRequired,
	ErrTokenInvalidOrExpired,
	ErrTokenValidateFailed,
	ErrTokenExpiryParseFailed,
	ErrDBTransactionBeginFailed,
	ErrTokenDeleteFailed,
	ErrJobDeleteFailed,
	ErrVideoDeleteFailed,
	ErrDBTransactionCommitFailed,
	ErrVideoVisibilityRequired,
	ErrVideoVisibilityInvalid,
	ErrVideoVisibilityUpdateFailed,
	ErrTokenGenerateFailed,
	ErrTokenStoreFailed,
	ErrRefererNotAllowed,
	ErrVideoReferrerWhitelistInvalid,
	ErrVideoReferrerWhitelistUpdateFailed,
	ErrVideoNotReady,
	ErrVideoOriginalNotFound,
	ErrStreamAssetNotFound,
	ErrVideoQueryFailed,
	ErrVideoScanFailed,
	ErrVideoIterateFailed,
	ErrJobQueryFailed,
	ErrJobScanFailed,
	ErrJobIterateFailed,
	ErrThumbnailNotFound,
	ErrJobProcessingFailed,
}

var byMessage = func() map[string]Error {
	m := make(map[string]Error, len(catalog))
	for _, e := range catalog {
		if _, exists := m[e.Message]; exists {
			panic("errcode: duplicate catalog message: " + e.Message)
		}
		m[e.Message] = e
	}
	return m
}()

func FromMessage(message string) Error {
	if e, ok := byMessage[message]; ok {
		return e
	}
	log.Printf("errcode: unmapped message %q, fallback code=%s", message, ErrInternalError.Code)
	return ErrInternalError
}
