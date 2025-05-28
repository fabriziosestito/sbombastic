package messaging

type Message interface {
	MessageType() string
}

const (
	CreateCatalogType = "CreateCatalog"
	GenerateSBOMType  = "GenerateSBOM"
	ScanSBOMType      = "ScanSBOM"
)

type CreateCatalog struct {
	ScanJobName      string `json:"scanJobName"`
	ScanJobNamespace string `json:"scanJobNamespace"`
}

func (m *CreateCatalog) MessageType() string {
	return CreateCatalogType
}

type GenerateSBOM struct {
	ScanJobName      string `json:"scanJobName"`
	ScanJobNamespace string `json:"scanJobNamespace"`
	ImageName        string `json:"imageName"`
	ImageNamespace   string `json:"imageNamespace"`
}

func (m *GenerateSBOM) MessageType() string {
	return GenerateSBOMType
}

type ScanSBOM struct {
	ScanJobName      string `json:"scanJobName"`
	ScanJobNamespace string `json:"scanJobNamespace"`
	SBOMName         string `json:"sbomName"`
	SBOMNamespace    string `json:"sbomNamespace"`
}

func (m *ScanSBOM) MessageType() string {
	return ScanSBOMType
}
