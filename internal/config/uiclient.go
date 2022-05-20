package config

type UIClient struct {
	FrontendURL              string `yaml:"frontend_url" envconfig:"FRONTEND_URL"`
	KeycloakOpenIDConnectURL string `yaml:"keycloak_open_id_connect_url" envconfig:"KEYCLOAK_OPENID_CONNECT_URL"`
	KeycloakOpenIDClientID   string `yaml:"keycloak_open_id_client_id" envconfig:"KEYCLOAK_OPENID_CLIENT_ID"`
	KeycloakOpenIDScope      string `yaml:"keycloak_open_id_scope" envconfig:"KEYCLOAK_OPENID_SCOPE"`
	HydraOpenIDConnectURL    string `yaml:"hydra_open_id_connect_url" envconfig:"HYDRA_OPENID_CONNECT_URL"`
	HydraOpenIDClientID      string `yaml:"hydra_open_id_client_id" envconfig:"HYDRA_OPENID_CLIENT_ID"`
	HydraOpenIDGuestClientID string `yaml:"hydra_open_id_guest_client_id" envconfig:"HYDRA_OPENID_GUEST_CLIENT_ID"`
	HydraOpenIDScope         string `yaml:"hydra_open_id_scope" envconfig:"HYDRA_OPENID_SCOPE"`
	Web3IdentityProviderURL  string `yaml:"web_3_identity_provider_url" envconfig:"WEB3_IDENTITY_PROVIDER_URL"`
	GuestIdentityProviderURL string `yaml:"guest_identity_provider_url" envconfig:"GUEST_IDENTITY_PROVIDER_URL"`
	SentryDSN                string `yaml:"sentry_dsn" envconfig:"SENTRY_DSN"`
	AgoraAppID               string `yaml:"agora_app_id" envconfig:"AGORA_APP_ID"`
	AuthServiceURL           string `yaml:"auth_service_url" envconfig:"AUTH_SERVICE_URL"`
	GoogleAPIClientID        string `yaml:"google_api_client_id" envconfig:"GOOGLE_API_CLIENT_ID"`
	GoogleAPIDeveloperKey    string `yaml:"google_api_developer_key" envconfig:"GOOGLE_API_DEVELOPER_KEY"`
	MiroAppID                string `yaml:"miro_app_id" envconfig:"MIRO_APP_ID"`
	ReactAppYoutubeKey       string `yaml:"react_app_youtube_key" envconfig:"REACT_APP_YOUTUBE_KEY"`
}

func (c *UIClient) Init() {

}
