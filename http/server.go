package http

import (
	"encoding/json"
	"errors"
	"github.com/smarterclayton/geard/config"
	"github.com/smarterclayton/geard/dispatcher"
	"github.com/smarterclayton/geard/gears"
	"github.com/smarterclayton/geard/jobs"
	"github.com/smarterclayton/go-json-rest"
	"io"
	"log"
	"net/http"
)

var ErrHandledResponse = errors.New("Request handled")

type HttpConfiguration struct {
	Docker     config.DockerConfiguration
	Dispatcher *dispatcher.Dispatcher
	Extensions []HttpExtension
}

type RestRoute struct {
	Method  string
	Path    string
	Handler JobHandler
}

type HttpExtension func() []RestRoute

func (conf *HttpConfiguration) Handler() http.Handler {
	handler := rest.ResourceHandler{
		EnableRelaxedContentType: true,
		EnableResponseStackTrace: true,
		EnableGzip:               false,
	}

	handlers := []rest.Route{
		rest.Route{"GET", "/token/:token/containers", conf.jobRestHandler(apiListContainers)},
		rest.Route{"PUT", "/token/:token/containers/links", conf.jobRestHandler(apiPutContainerLinks)},
		rest.Route{"PUT", "/token/:token/container", conf.jobRestHandler(apiPutContainer)},
		rest.Route{"DELETE", "/token/:token/container", conf.jobRestHandler(apiDeleteContainer)},
		rest.Route{"GET", "/token/:token/container/log", conf.jobRestHandler(apiGetContainerLog)},
		rest.Route{"PUT", "/token/:token/container/:action", conf.jobRestHandler(apiPutContainerAction)},
		rest.Route{"GET", "/token/:token/container/ports", conf.jobRestHandler(apiGetContainerPorts)},

		rest.Route{"GET", "/token/:token/images", conf.jobRestHandler(conf.apiListImages)},

		rest.Route{"GET", "/token/:token/content", conf.jobRestHandler(apiGetContent)},
		rest.Route{"GET", "/token/:token/content/*", conf.jobRestHandler(apiGetContent)},

		rest.Route{"PUT", "/token/:token/keys", conf.jobRestHandler(apiPutKeys)},

		rest.Route{"PUT", "/token/:token/environment", conf.jobRestHandler(apiPutEnvironment)},
		rest.Route{"PATCH", "/token/:token/environment", conf.jobRestHandler(apiPatchEnvironment)},

		rest.Route{"GET", "/token/:token/builds", conf.jobRestHandler(apiListBuilds)},
		rest.Route{"PUT", "/token/:token/build-image", conf.jobRestHandler(apiPutBuildImageAction)},
	}

	for i := range conf.Extensions {
		routes := conf.Extensions[i]()
		for j := range routes {
			handlers = append(handlers, rest.Route{routes[j].Method, routes[j].Path, conf.jobRestHandler(routes[j].Handler)})
		}
	}
	handler.SetRoutes(handlers...)
	return &handler
}

type JobHandler func(jobs.RequestIdentifier, *TokenData, *rest.ResponseWriter, *rest.Request) (jobs.Job, error)

func (conf *HttpConfiguration) jobRestHandler(handler JobHandler) func(*rest.ResponseWriter, *rest.Request) {
	return func(w *rest.ResponseWriter, r *rest.Request) {
		token, id, errt := extractToken(r.PathParam("token"), r.Request)
		if errt != nil {
			log.Println(errt)
			http.Error(w, "Token is required - pass /token/<token>/<path>", http.StatusForbidden)
			return
		}

		if token.D == 0 {
			log.Println("http: Recommend passing 'd' as an argument for the current date")
		}
		if token.U == "" {
			log.Println("http: Recommend passing 'u' as an argument for the associated user")
		}

		job, errh := handler(id, token, w, r)
		if errh != nil {
			if errh != ErrHandledResponse {
				http.Error(w, "Invalid request: "+errh.Error()+"\n", http.StatusBadRequest)
			}
			return
		}

		wait, errd := conf.Dispatcher.Dispatch(job)
		if errd == jobs.ErrRanToCompletion {
			http.Error(w, errd.Error(), http.StatusNoContent)
			return
		} else if errd != nil {
			serveRequestError(w, apiRequestError{errd, errd.Error(), http.StatusServiceUnavailable})
			return
		}
		<-wait
	}
}

func apiPutContainer(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	gearId, errg := gears.NewIdentifier(token.ResourceLocator())
	if errg != nil {
		return nil, errg
	}
	if token.ResourceType() == "" {
		return nil, errors.New("A container must have an image identifier")
	}

	data := jobs.ExtendedInstallContainerData{}
	if r.Body != nil {
		dec := json.NewDecoder(limitedBodyReader(r))
		if err := dec.Decode(&data); err != nil && err != io.EOF {
			return nil, err
		}
	}
	if data.Ports == nil {
		data.Ports = make([]gears.PortPair, 0)
	}

	if data.Environment != nil {
		env := data.Environment
		if env.Id == gears.InvalidIdentifier {
			return nil, errors.New("You must specify an environment identifier on creation.")
		}
	}

	if data.NetworkLinks != nil {
		if err := data.NetworkLinks.Check(); err != nil {
			return nil, err
		}
	}

	return &jobs.InstallContainerRequest{
		NewHttpJobResponse(w.ResponseWriter, false),
		jobs.JobRequest{reqid},
		gearId,
		token.U,
		token.ResourceType(),
		&data,
	}, nil
}

func apiDeleteContainer(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	gearId, errg := gears.NewIdentifier(token.ResourceLocator())
	if errg != nil {
		return nil, errg
	}
	return &jobs.DeleteContainerRequest{NewHttpJobResponse(w.ResponseWriter, false), jobs.JobRequest{reqid}, gearId}, nil
}

func apiListBuilds(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	return &jobs.ListBuildsRequest{NewHttpJobResponse(w.ResponseWriter, false), jobs.JobRequest{reqid}}, nil
}

func apiListContainers(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	return &jobs.ListContainersRequest{NewHttpJobResponse(w.ResponseWriter, false), jobs.JobRequest{reqid}}, nil
}

func (conf HttpConfiguration) apiListImages(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	return &jobs.ListImagesRequest{NewHttpJobResponse(w.ResponseWriter, false), jobs.JobRequest{reqid}, conf.Docker.Socket}, nil
}

func apiGetContainerLog(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	gearId, errg := gears.NewIdentifier(token.ResourceLocator())
	if errg != nil {
		return nil, errg
	}
	return &jobs.ContainerLogRequest{
		NewHttpJobResponse(w.ResponseWriter, false),
		jobs.JobRequest{reqid},
		gearId,
		token.U,
	}, nil
}

func apiGetContainerPorts(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	gearId, errg := gears.NewIdentifier(token.ResourceLocator())
	if errg != nil {
		return nil, errg
	}
	return &jobs.ContainerPortsJobRequest{
		NewHttpJobResponse(w.ResponseWriter, false),
		jobs.JobRequest{reqid},
		gearId,
		token.U,
	}, nil
}

func apiPutKeys(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	data := jobs.ExtendedCreateKeysData{}
	if r.Body != nil {
		dec := json.NewDecoder(limitedBodyReader(r))
		if err := dec.Decode(&data); err != nil && err != io.EOF {
			return nil, err
		}
	}
	if err := data.Check(); err != nil {
		return nil, err
	}
	return &jobs.CreateKeysRequest{
		NewHttpJobResponse(w.ResponseWriter, true),
		jobs.JobRequest{reqid},
		token.U,
		&data,
	}, nil
}

func apiPutContainerAction(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	action := r.PathParam("action")
	gearId, errg := gears.NewIdentifier(token.ResourceLocator())
	if errg != nil {
		return nil, errg
	}
	switch action {
	case "started":
		return &jobs.StartedContainerStateRequest{
			NewHttpJobResponse(w.ResponseWriter, false),
			jobs.JobRequest{reqid},
			gearId,
			token.U,
		}, nil
	case "stopped":
		return &jobs.StoppedContainerStateRequest{
			NewHttpJobResponse(w.ResponseWriter, false),
			jobs.JobRequest{reqid},
			gearId,
			token.U,
		}, nil
	default:
		return nil, errors.New("You must provide a valid action for this container to take")
	}
}

func apiPutBuildImageAction(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	if token.ResourceLocator() == "" {
		return nil, errors.New("You must specifiy the application source to build")
	}
	if token.ResourceType() == "" {
		return nil, errors.New("You must specify a base image")
	}

	source := token.ResourceLocator() // token.R
	baseImage := token.ResourceType() // token.T
	tag := token.U

	data := jobs.ExtendedBuildImageData{}
	if r.Body != nil {
		dec := json.NewDecoder(r.Body)
		if err := dec.Decode(&data); err != nil && err != io.EOF {
			return nil, err
		}
	}

	return &jobs.BuildImageRequest{
		NewHttpJobResponse(w.ResponseWriter, false),
		jobs.JobRequest{reqid},
		source,
		baseImage,
		tag,
		&data,
	}, nil
}

func apiPutEnvironment(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	id, errg := gears.NewIdentifier(token.ResourceLocator())
	if errg != nil {
		return nil, errg
	}

	data := jobs.ExtendedEnvironmentData{}
	if r.Body != nil {
		dec := json.NewDecoder(limitedBodyReader(r))
		if err := dec.Decode(&data); err != nil && err != io.EOF {
			return nil, err
		}
	}
	if err := data.Check(); err != nil {
		return nil, err
	}
	data.Id = id

	return &jobs.PutEnvironmentRequest{
		NewHttpJobResponse(w.ResponseWriter, false),
		jobs.JobRequest{reqid},
		&data,
	}, nil
}

func apiPatchEnvironment(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	id, errg := gears.NewIdentifier(token.ResourceLocator())
	if errg != nil {
		return nil, errg
	}

	data := jobs.ExtendedEnvironmentData{}
	if r.Body != nil {
		dec := json.NewDecoder(limitedBodyReader(r))
		if err := dec.Decode(&data); err != nil && err != io.EOF {
			return nil, err
		}
	}
	if err := data.Check(); err != nil {
		return nil, err
	}
	data.Id = id

	return &jobs.PatchEnvironmentRequest{
		NewHttpJobResponse(w.ResponseWriter, false),
		jobs.JobRequest{reqid},
		&data,
	}, nil
}

func apiGetContent(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	if token.ResourceLocator() == "" {
		return nil, errors.New("You must specify the location of the content you want to access")
	}
	if token.ResourceType() == "" {
		return nil, errors.New("You must specify the type of the content you want to access")
	}

	return &jobs.ContentRequest{
		NewHttpJobResponse(w.ResponseWriter, false),
		jobs.JobRequest{reqid},
		token.ResourceType(),
		token.ResourceLocator(),
		r.PathParam("*"),
	}, nil
}

func apiPutContainerLinks(reqid jobs.RequestIdentifier, token *TokenData, w *rest.ResponseWriter, r *rest.Request) (jobs.Job, error) {
	data := jobs.ExtendedLinkContainersData{}
	if r.Body != nil {
		dec := json.NewDecoder(limitedBodyReader(r))
		if err := dec.Decode(&data); err != nil && err != io.EOF {
			return nil, err
		}
	}

	if err := data.Check(); err != nil {
		return nil, err
	}

	return &jobs.LinkContainersRequest{
		NewHttpJobResponse(w.ResponseWriter, false),
		jobs.JobRequest{reqid},
		&data,
	}, nil
}

func limitedBodyReader(r *rest.Request) io.Reader {
	return io.LimitReader(r.Body, 100*1024)
}

func extractToken(segment string, r *http.Request) (token *TokenData, id jobs.RequestIdentifier, rerr *apiRequestError) {
	if segment == "__test__" {
		t, err := NewTokenFromMap(r.URL.Query())
		if err != nil {
			rerr = &apiRequestError{err, "Invalid test query: " + err.Error(), http.StatusForbidden}
			return
		}
		token = t
	} else {
		t, err := NewTokenFromString(segment)
		if err != nil {
			rerr = &apiRequestError{err, "Invalid authorization token", http.StatusForbidden}
			return
		}
		token = t
	}

	if token.I == "" {
		id = jobs.NewRequestIdentifier()
	} else {
		i, errr := token.RequestId()
		if errr != nil {
			rerr = &apiRequestError{errr, "Unable to parse token for this request: " + errr.Error(), http.StatusBadRequest}
			return
		}
		id = i
	}

	return
}

type apiRequestError struct {
	Error   error
	Message string
	Status  int
}

func serveRequestError(w http.ResponseWriter, err apiRequestError) {
	log.Print(err.Message, err.Error)
	http.Error(w, err.Message, err.Status)
}