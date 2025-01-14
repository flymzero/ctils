package ali

import (
	"fmt"
	"mime/multipart"
	"sort"
	"strings"
	"sync"
	"time"

	json "github.com/json-iterator/go"

	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/xurwxj/ctils/log"
	"github.com/xurwxj/ctils/oss/utils"
	"github.com/xurwxj/ctils/sessions"
)

type BucketInfo struct {
	OsBucket map[string]*oss.Bucket
	Lock     sync.Mutex
}

var bsCS BucketInfo

var bs map[string]*oss.Bucket

var chunkBS map[string]int
var imurs map[string]oss.InitiateMultipartUploadResult
var chunkIMURS map[string]int
var chunkParts map[string]int
var completedParts map[string][]oss.UploadPart

func init() {
	fmt.Println("ali oss upload init() starting...")
	if bsCS.OsBucket == nil || len(bsCS.OsBucket) < 1 {
		bsCS = BucketInfo{
			OsBucket: make(map[string]*oss.Bucket),
		}
	}
	if bs == nil || len(bs) < 1 {
		bs = make(map[string]*oss.Bucket)
	}
	if chunkBS == nil || len(chunkBS) < 1 {
		chunkBS = make(map[string]int)
	}
	if imurs == nil || len(imurs) < 1 {
		imurs = make(map[string]oss.InitiateMultipartUploadResult)
	}
	if chunkIMURS == nil || len(chunkIMURS) < 1 {
		chunkIMURS = make(map[string]int)
	}
	if chunkParts == nil || len(chunkParts) < 1 {
		chunkParts = make(map[string]int)
	}
	if completedParts == nil || len(completedParts) < 1 {
		completedParts = make(map[string][]oss.UploadPart, 1)
	}
}

// ChunkUploadGetStream chunk upload for get
// 400 NotExist client should call post to upload this part
// 200 OK client need to ignore this part
// 300 dfsID caller should do other thing, then client can get success upload infor
// 500 client need to upload whole file again
func ChunkUploadGetStream(userID, prefer, cloud string, chunk utils.ChunksObj) (utils.ChunksObj, int, string, error) {
	dfsID := utils.SetMultiPartDfsID(userID, cloud, chunk)
	if !checkPartNumberUploaded(chunk.ChunkNumber, dfsID) {
		return chunk, 400, "NotExist", nil
	}
	if checkAllPartsUploaded(chunk.TotalChunks, dfsID) {
		return completeChunksUpload(userID, prefer, dfsID, chunk)
	}
	return chunk, 200, "OK", nil
}

func ChunkUploadGetStreamCS(userID, prefer, cloud string, chunk utils.ChunksObj) (utils.ChunksObj, int, string, error) {
	dfsID := utils.SetMultiPartDfsID(userID, cloud, chunk)
	if !checkPartNumberUploadedCS(chunk.ChunkNumber, dfsID) {
		return chunk, 400, "NotExist", nil
	}
	if checkAllPartsUploadedCS(chunk.TotalChunks, dfsID) {
		return completeChunksUploadCS(userID, prefer, dfsID, chunk)
	}
	return chunk, 200, "OK", nil
}

// ChunkUploadPostStream chunk upload for post
// 400 NotExist client should call post to upload this part
// 200 OK client need to ignore this part
// 300 dfsID caller should do other thing, then client can get success upload infor
// 500 client need to upload whole file again
func ChunkUploadPostStream(userID, prefer, cloud string, chunk utils.ChunksObj, fileChunk *multipart.FileHeader) (utils.ChunksObj, int, string, error) {
	dfsID := utils.SetMultiPartDfsID(userID, cloud, chunk)
	b, err := getBucketInstance(prefer, chunk.Bucket, dfsID, chunk.ChunkNumber)
	if err != nil {
		return chunk, 400, "NotExist", err
	}
	imur, err := getIMURS(dfsID, chunk.ChunkNumber, b)
	if err != nil {
		return chunk, 400, "NotExist", err
	}
	options := []oss.Option{
		oss.ContentDisposition("filename=" + chunk.Filename),
	}
	f, err := fileChunk.Open()
	if err != nil {
		return chunk, 400, "NotExist", err
	}
	defer f.Close()
	chunkPart, err := b.UploadPart(imur, f, chunk.CurrentChunkSize, chunk.ChunkNumber, options...)
	if err != nil {
		if strings.Index(err.Error(), "dial tcp") > -1 {
			return ChunkUploadPostStream(userID, prefer, cloud, chunk, fileChunk)
		}
		return chunk, 400, "NotExist", err
	}
	f.Close()
	err = setCompletePart(chunkPart, dfsID, chunk.ChunkNumber)
	if err != nil {
		return chunk, 400, "NotExist", err
	}
	if checkAllPartsUploaded(chunk.TotalChunks, dfsID) {
		return completeChunksUpload(userID, prefer, dfsID, chunk)
	}
	return chunk, 200, "OK", nil
}

func ChunkUploadPostStreamCS(userID, prefer, cloud string, chunk utils.ChunksObj, fileChunk *multipart.FileHeader) (utils.ChunksObj, int, string, error) {
	dfsID := utils.SetMultiPartDfsID(userID, cloud, chunk)
	b, err := getBucketInstanceCS(prefer, chunk.Bucket, dfsID, chunk.ChunkNumber)
	log.Log.Debug().Interface("b", b).Msg("ChunkUploadPostStreamCS")
	if err != nil {
		return chunk, 400, "NotExist", err
	}
	imur, err := getIMURSCS(dfsID, chunk.ChunkNumber, b)
	log.Log.Debug().Interface("imur", imur).Msg("ChunkUploadPostStreamCS")

	if err != nil {
		return chunk, 400, "NotExist", err
	}
	options := []oss.Option{
		oss.ContentDisposition("filename=" + chunk.Filename),
	}
	f, err := fileChunk.Open()
	if err != nil {
		return chunk, 400, "NotExist", err
	}
	defer f.Close()
	chunkPart, err := b.UploadPart(imur, f, chunk.CurrentChunkSize, chunk.ChunkNumber, options...)
	if err != nil {
		if strings.Index(err.Error(), "dial tcp") > -1 {
			return ChunkUploadPostStreamCS(userID, prefer, cloud, chunk, fileChunk)
		}
		return chunk, 400, "NotExist", err
	}
	f.Close()
	err = setCompletePartCS(chunkPart, dfsID, chunk.ChunkNumber)
	if err != nil {
		return chunk, 400, "NotExist", err
	}
	if checkAllPartsUploadedCS(chunk.TotalChunks, dfsID) {
		return completeChunksUploadCS(userID, prefer, dfsID, chunk)
	}
	return chunk, 200, "OK", nil
}

func setCompletePart(part oss.UploadPart, dfsID string, chunkNumber int) error {
	cp, has := chunkParts[dfsID]
	if cp > 0 && has {
		time.Sleep(1 * time.Second)
		return setCompletePart(part, dfsID, chunkNumber)
	}
	chunkParts[dfsID] = chunkNumber
	allParts, h := completedParts[dfsID]
	if !h || len(allParts) < 1 {
		allParts = append(allParts, part)
	} else {
		e := false
		for k, p := range allParts {
			if p.PartNumber == part.PartNumber {
				allParts[k] = part
				e = true
			}
		}
		if !e {
			allParts = append(allParts, part)
		}
	}
	if len(allParts) > 1 {
		sort.Slice(allParts, func(i, j int) bool {
			return allParts[i].PartNumber-allParts[j].PartNumber < 0
		})
	}
	completedParts[dfsID] = allParts
	delete(chunkParts, dfsID)
	return nil
}

func setCompletePartCS(part oss.UploadPart, dfsID string, chunkNumber int) error {
	cp := sessions.SESS.GetChunkParts(dfsID)
	if cp > 0 {
		time.Sleep(1 * time.Second)
		return setCompletePartCS(part, dfsID, chunkNumber)
	}
	sessions.SESS.SetChunkParts(dfsID, chunkNumber)
	tallParts := sessions.SESS.GetCompletePart(dfsID)
	allParts := make([]oss.UploadPart, 0)
	if err := json.Unmarshal(tallParts, &allParts); err != nil {
		log.Log.Err(err).Str("tallParts", string(tallParts)).Str("key", dfsID).Msg("setCompletePartCS:Unmarshal")
	}
	if len(allParts) < 1 {
		allParts = append(allParts, part)
	} else {
		e := false
		for k, p := range allParts {
			if p.PartNumber == part.PartNumber {
				allParts[k] = part
				e = true
			}
		}
		if !e {
			allParts = append(allParts, part)
		}
	}
	if len(allParts) > 1 {
		sort.Slice(allParts, func(i, j int) bool {
			return allParts[i].PartNumber-allParts[j].PartNumber < 0
		})
	}
	sessions.SESS.SetCompletePart(dfsID, allParts)
	sessions.SESS.DelChunkParts(dfsID)
	return nil
}

func completeChunksUpload(userID, prefer, dfsID string, chunk utils.ChunksObj) (utils.ChunksObj, int, string, error) {
	allParts, has := completedParts[dfsID]
	if !has || len(allParts) != chunk.TotalChunks {
		go clearInit(dfsID)
		return chunk, 500, "completePartsErr", nil
	}
	b, err := getBucketInstance(prefer, chunk.Bucket, dfsID, chunk.ChunkNumber)
	if err != nil {
		go clearInit(dfsID)
		return chunk, 500, "connectionErr", err
	}
	imur, err := getIMURS(dfsID, chunk.ChunkNumber, b)
	if err != nil {
		go clearInit(dfsID)
		return chunk, 500, "imurErr", err
	}
	options := []oss.Option{
		oss.ContentDisposition("filename=" + chunk.Filename),
	}
	_, err = b.CompleteMultipartUpload(imur, allParts, options...)
	if err != nil {
		go clearInit(dfsID)
		return chunk, 500, "completeErr", err
	}
	err = b.SetObjectMeta(dfsID, options...)
	if h, err := b.GetObjectDetailedMeta(dfsID); err == nil {
		chunk.ContentType = h.Get("Content-Type")
	}
	chunk.Bucket = b.BucketName
	chunk.Endpoint = utils.GetEndpointByPrefer(prefer)
	chunk.DfsID = dfsID
	if chunk.DownValidTo > 0 {
		var urlRS map[string]string
		urlRS, err = GetTempDownURLFileName(b.BucketName, dfsID, chunk.DownValidTo)
		if url, has := urlRS["url"]; has && url != "" {
			chunk.DownURL = url
		}
	}
	go clearInit(dfsID)
	return chunk, 200, "DONE", err
}

func completeChunksUploadCS(userID, prefer, dfsID string, chunk utils.ChunksObj) (utils.ChunksObj, int, string, error) {
	tallParts := sessions.SESS.GetCompletePart(dfsID)
	allParts := make([]oss.UploadPart, 0)
	if err := json.Unmarshal(tallParts, &allParts); err != nil {
		log.Log.Err(err).Str("tallParts", string(tallParts)).Str("key", dfsID).Msg("completeChunksUploadCS:Unmarshal")
	}

	if len(allParts) != chunk.TotalChunks {
		clearInitCS(dfsID)
		return chunk, 500, "completePartsErr", nil
	}
	b, err := getBucketInstanceCS(prefer, chunk.Bucket, dfsID, chunk.ChunkNumber)
	if err != nil {
		clearInitCS(dfsID)
		return chunk, 500, "connectionErr", err
	}
	imur, err := getIMURSCS(dfsID, chunk.ChunkNumber, b)
	if err != nil {
		clearInitCS(dfsID)
		return chunk, 500, "imurErr", err
	}
	options := []oss.Option{
		oss.ContentDisposition("filename=" + chunk.Filename),
	}
	log.Log.Debug().Interface("b", b).Interface("dfsID", dfsID).Interface("imur", imur).Interface("allParts", allParts).Interface("options", options).Msg("completeChunksUploadCS:CompleteMultipartUpload")
	_, err = b.CompleteMultipartUpload(imur, allParts, options...)
	if err != nil {
		clearInitCS(dfsID)
		return chunk, 500, "completeErr", err
	}
	err = b.SetObjectMeta(dfsID, options...)
	if h, err := b.GetObjectDetailedMeta(dfsID); err == nil {
		chunk.ContentType = h.Get("Content-Type")
	}
	chunk.Bucket = b.BucketName
	chunk.Endpoint = utils.GetEndpointByPrefer(prefer)
	chunk.DfsID = dfsID
	if chunk.DownValidTo > 0 {
		var urlRS map[string]string
		urlRS, err = GetTempDownURLFileName(b.BucketName, dfsID, chunk.DownValidTo)
		if url, has := urlRS["url"]; has && url != "" {
			chunk.DownURL = url
		}
	}
	go clearInitCS(dfsID)
	return chunk, 200, "DONE", err
}
func clearInit(dfsID string) {
	delete(bs, dfsID)
	delete(chunkBS, dfsID)
	delete(imurs, dfsID)
	delete(chunkIMURS, dfsID)
	delete(chunkParts, dfsID)
	delete(completedParts, dfsID)
}

func clearInitCS(dfsID string) {
	sessions.SESS.DelAllParts(dfsID)
	bsCS.Lock.Lock()
	defer bsCS.Lock.Unlock()
	delete(bsCS.OsBucket, dfsID)
}
func getIMURS(dfsID string, chunkNumber int, b *oss.Bucket) (oss.InitiateMultipartUploadResult, error) {
	t, h := chunkIMURS[dfsID]
	imur, has := imurs[dfsID]
	if t > 0 && h && (imur.UploadID == "" || !has) {
		time.Sleep(1 * time.Second)
		return getIMURS(dfsID, chunkNumber, b)
	}
	//TODO b判断有问题
	if has && b != nil {
		return imur, nil
	}
	chunkIMURS[dfsID] = chunkNumber
	imur, err := b.InitiateMultipartUpload(dfsID)
	if err != nil {
		return oss.InitiateMultipartUploadResult{}, err
	}
	imurs[dfsID] = imur
	return imur, nil
}

func getIMURSCS(dfsID string, chunkNumber int, b *oss.Bucket) (oss.InitiateMultipartUploadResult, error) {
	t := sessions.SESS.GetChunkIMURS(dfsID)
	timur := sessions.SESS.GetImurs(dfsID)
	imur := oss.InitiateMultipartUploadResult{}
	if err := json.Unmarshal(timur, &imur); err != nil {
		log.Log.Err(err).Str("timur", string(timur)).Str("key", dfsID).Msg("getIMURSCS:Unmarshal")
	}
	if t > 0 && (imur.UploadID == "") {
		time.Sleep(1 * time.Second)
		return getIMURSCS(dfsID, chunkNumber, b)
	}

	if imur.UploadID != "" {
		return imur, nil
	}
	sessions.SESS.SetChunkIMURS(dfsID, chunkNumber)
	imur, err := b.InitiateMultipartUpload(dfsID)
	if err != nil {
		return oss.InitiateMultipartUploadResult{}, err
	}
	log.Log.Debug().Interface("imurid", imur).Interface("dfsID", dfsID).Msg("getIMURSCS")
	sessions.SESS.SetImurs(dfsID, imur)
	return imur, nil
}

func getBucketInstance(prefer, bucketType, dfsID string, chunkNumber int) (*oss.Bucket, error) {
	t, h := chunkBS[dfsID]
	b, has := bs[dfsID]
	if t > 0 && h && (b == nil || !has) {
		time.Sleep(1 * time.Second)
		return getBucketInstance(prefer, bucketType, dfsID, chunkNumber)
	}
	if has && b != nil {
		return b, nil
	}
	chunkBS[dfsID] = chunkNumber
	prefer, bucket := utils.GetByBucketPrefer(prefer, bucketType)
	b, err := InitBucket(prefer, bucket)
	if err != nil {
		return nil, err
	}
	bs[dfsID] = b
	return b, nil
}

func getBucketInstanceCS(prefer, bucketType, dfsID string, chunkNumber int) (*oss.Bucket, error) {
	t := sessions.SESS.GetChunkBS(dfsID)
	bsCS.Lock.Lock()
	defer bsCS.Lock.Unlock()
	log.Log.Debug().Interface("OsBucket", bsCS.OsBucket).Msg("getBucketInstanceCS:OsBucket")
	b, has := bsCS.OsBucket[dfsID]
	if t > 0 && (b == nil || !has) {
		time.Sleep(1 * time.Second)
		return getBucketInstanceCS(prefer, bucketType, dfsID, chunkNumber)
	}
	if has && b != nil {
		return b, nil
	}
	log.Log.Debug().Interface("b", b).Str("b", dfsID).Msg("getBucketInstanceCS:OsBucket")
	sessions.SESS.SetChunkBS(dfsID, chunkNumber)
	prefer, bucket := utils.GetByBucketPrefer(prefer, bucketType)
	b, err := InitBucket(prefer, bucket)
	if err != nil {
		return nil, err
	}
	bsCS.OsBucket[dfsID] = b
	return b, nil
}

func checkAllPartsUploaded(totals int, dfsID string) bool {
	cps, has := completedParts[dfsID]
	if has && len(cps) == totals {
		return true
	}
	return false
}

func checkAllPartsUploadedCS(totals int, dfsID string) bool {
	log.Log.Debug().Interface("dfsID", dfsID).Msg("checkAllPartsUploadedCS")
	tallParts := sessions.SESS.GetCompletePart(dfsID)
	allParts := make([]oss.UploadPart, 0)
	if err := json.Unmarshal(tallParts, &allParts); err != nil {
		log.Log.Err(err).Str("tallParts", string(tallParts)).Str("key", dfsID).Msg("checkAllPartsUploadedCS:Unmarshal")
	}
	return len(allParts) == totals
}

func checkPartNumberUploaded(chunkNumber int, dfsID string) bool {
	cps, has := completedParts[dfsID]
	if has && len(cps) > 0 {
		for _, cp := range cps {
			if cp.PartNumber == chunkNumber {
				return true
			}
		}
	}
	return false
}

func checkPartNumberUploadedCS(chunkNumber int, dfsID string) bool {
	cps := sessions.SESS.GetCompletePart(dfsID)
	allPorts := make([]oss.UploadPart, 0)
	if err := json.Unmarshal(cps, &allPorts); err != nil {
		log.Log.Err(err).Str("cps", string(cps)).Str("key", dfsID).Msg("checkPartNumberUploadedCS:Unmarshal")
	}
	if len(allPorts) > 0 {
		for _, port := range allPorts {
			if port.PartNumber == chunkNumber {
				return true
			}
		}
	}
	return false
}
