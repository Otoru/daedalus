package daedalus

const (
	// splitMixGamma é o incremento ímpar fixado pelo algoritmo SplitMix64.
	splitMixGamma uint64 = 0x9E3779B97F4A7C15
	// splitMixFirstMultiplier é o primeiro multiplicador da finalização SplitMix64.
	splitMixFirstMultiplier uint64 = 0xBF58476D1CE4E5B9
	// splitMixSecondMultiplier é o segundo multiplicador da finalização SplitMix64.
	splitMixSecondMultiplier uint64 = 0x94D049BB133111EB
	// splitMixFirstShift é o primeiro deslocamento lógico da finalização SplitMix64.
	splitMixFirstShift = 30
	// splitMixSecondShift é o segundo deslocamento lógico da finalização SplitMix64.
	splitMixSecondShift = 27
	// splitMixFinalShift é o último deslocamento lógico da finalização SplitMix64.
	splitMixFinalShift = 31

	// placementStreamSalt separa o stream de posicionamento dos demais streams.
	placementStreamSalt uint64 = 0xA0B1C2D3E4F56789
	// connectorStreamSalt separa o stream de conexão dos demais streams.
	connectorStreamSalt uint64 = 0x1F2E3D4C5B6A7988
	// roomPlantStreamSalt separa o stream de Plant de Room dos demais streams.
	// O valor coincide com splitMixGamma por determinação da seção 10.3, que
	// lista esta constante para RoomPlantSeed. A repetição é intencional e
	// congelada: trocá-la por outro valor mudaria todos os Layouts.
	roomPlantStreamSalt uint64 = 0x9E3779B97F4A7C15
	// roomGeometryStreamSalt separa o stream de geometria de Room dos demais streams.
	roomGeometryStreamSalt uint64 = 0x6C8E9CF570932BD5
	// corridorPlantStreamSalt separa o stream de Plant de Corridor dos demais streams.
	corridorPlantStreamSalt uint64 = 0xD1B54A32D192ED03

	// uniformMantissaBits é a quantidade de bits aleatórios exatamente
	// representáveis na mantissa usada por uniform01.
	uniformMantissaBits = 53
	// uniformDiscardedBits remove os bits inferiores que excedem a mantissa.
	uniformDiscardedBits = 64 - uniformMantissaBits
	// uniformDenominator é 2^53 e mantém o limite superior de uniform01 exclusivo.
	uniformDenominator = float64(uint64(1) << uniformMantissaBits)
)

// splitMix64 é um stream mutável e privado de uma única solicitação.
// Instâncias não devem ser compartilhadas entre goroutines.
type splitMix64 struct {
	state uint64
}

// rngStreams contém os cinco streams independentes de uma solicitação.
type rngStreams struct {
	placement     splitMix64
	connector     splitMix64
	roomPlant     splitMix64
	roomGeometry  splitMix64
	corridorPlant splitMix64
}

// mix64 aplica a finalização SplitMix64 congelada pela especificação. A
// aritmética uint64 faz wraparound módulo 2^64 de forma definida pelo Go.
func mix64(value uint64) uint64 {
	value += splitMixGamma
	value = (value ^ (value >> splitMixFirstShift)) * splitMixFirstMultiplier
	value = (value ^ (value >> splitMixSecondShift)) * splitMixSecondMultiplier
	return value ^ (value >> splitMixFinalShift)
}

func newSplitMix64(seed uint64) splitMix64 {
	return splitMix64{state: seed}
}

// newRNGStreams deriva todos os streams diretamente da Seed da solicitação.
func newRNGStreams(seed Seed) rngStreams {
	seedValue := uint64(seed)
	return rngStreams{
		placement:     newSplitMix64(mix64(seedValue ^ placementStreamSalt)),
		connector:     newSplitMix64(mix64(seedValue ^ connectorStreamSalt)),
		roomPlant:     newSplitMix64(mix64(seedValue ^ roomPlantStreamSalt)),
		roomGeometry:  newSplitMix64(mix64(seedValue ^ roomGeometryStreamSalt)),
		corridorPlant: newSplitMix64(mix64(seedValue ^ corridorPlantStreamSalt)),
	}
}

// next devolve o próximo valor do stream e avança seu estado uma única vez.
func (stream *splitMix64) next() uint64 {
	value := mix64(stream.state)
	stream.state += splitMixGamma
	return value
}

// uniform01 devolve um valor uniformemente distribuído no intervalo [0, 1).
func (stream *splitMix64) uniform01() float64 {
	value := stream.next() >> uniformDiscardedBits
	return float64(value) / uniformDenominator
}

// uniformInt devolve um inteiro uniformemente distribuído entre lower e
// upper, inclusive. A rejeição do prefixo incompleto elimina viés de módulo.
func (stream *splitMix64) uniformInt(lower, upper uint64) uint64 {
	if lower > upper {
		panic("limite inferior maior que o limite superior em uniformInt")
	}
	if lower == upper {
		return lower
	}

	rangeSize := upper - lower + 1
	if rangeSize == 0 {
		// O overflow representa exatamente todo o domínio uint64.
		return stream.next()
	}

	// Em aritmética uint64, -rangeSize é 2^64-rangeSize, portanto esta
	// expressão calcula 2^64 mod rangeSize: a quantidade de valores do
	// início do domínio que precisam ser descartados para que o restante
	// seja múltiplo exato de rangeSize. Sem esse descarte, os menores
	// valores sairiam com probabilidade maior.
	rejectionThreshold := -rangeSize % rangeSize
	for {
		value := stream.next()
		if value >= rejectionThreshold {
			return lower + value%rangeSize
		}
	}
}
