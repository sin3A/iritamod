package node

import (
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/bianjieai/iritamod/modules/node/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	"github.com/tendermint/tendermint/crypto/sm2"
	"github.com/tendermint/tendermint/crypto/sr25519"
	tmtypes "github.com/tendermint/tendermint/types"

	"github.com/cosmos/cosmos-sdk/codec/legacy"

	"github.com/cosmos/cosmos-sdk/codec"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	cautil "github.com/bianjieai/iritamod/utils/ca"
)

// InitGenesis - store genesis validator set
func InitGenesis(ctx sdk.Context, cdc codec.Codec, k Keeper, data GenesisState) (res []abci.ValidatorUpdate) {
	if err := ValidateGenesis(data); err != nil {
		panic(err.Error())
	}

	k.SetParams(ctx, data.Params)
	k.SetRootCert(ctx, data.RootCert)
	keyCdc := newTmCryptoCodec()

	for _, val := range data.Validators {
		k.SetValidator(ctx, val)
		pubkey := TmPk2ProtoPk(val.Pubkey, keyCdc)
		val.Pubkey = pubkey
		var pk cryptotypes.PubKey
		bz, err := sdk.GetFromBech32(val.Pubkey, sdk.GetConfig().GetBech32ConsensusPubPrefix())
		pk, err = legacy.PubKeyFromBytes(bz)
		if err != nil {
			panic(err)
		}
		id, err := hex.DecodeString(val.Id)
		if err != nil {
			panic(err)
		}

		k.SetValidatorConsAddrIndex(ctx, id, sdk.GetConsAddress(pk))

		pubKey, err := val.ConsPubKey()
		if err != nil {
			panic(err)
		}
		tmPubKey, err := cryptocodec.ToTmPubKeyInterface(pubKey)
		if err != nil {
			panic(err)
		}
		res = append(res, ABCIValidatorUpdate(
			tmPubKey,
			val.Power,
		))
	}

	for _, node := range data.Nodes {
		id, _ := hex.DecodeString(node.Id)
		k.SetNode(ctx, id, node)
	}

	return
}

// ExportGenesis - output genesis valiadtor set
func ExportGenesis(ctx sdk.Context, k Keeper) *GenesisState {
	rootCert, _ := k.GetRootCert(ctx)
	return NewGenesisState(rootCert, k.GetParams(ctx), k.GetAllValidators(ctx), k.GetNodes(ctx))
}

// WriteValidators returns a slice of bonded genesis validators.
func WriteValidators(ctx sdk.Context, keeper Keeper) (vals []tmtypes.GenesisValidator) {
	for _, v := range keeper.GetLastValidators(ctx) {
		consPk, err := v.ConsPubKey()
		if err != nil {
			continue
		}
		tmPubkey, err := cryptocodec.ToTmPubKeyInterface(consPk)
		if err != nil {
			continue
		}
		vals = append(vals, tmtypes.GenesisValidator{
			PubKey: tmPubkey,
			Power:  v.GetConsensusPower(sdk.DefaultPowerReduction),
			Name:   v.GetMoniker(),
		})
	}

	return
}

// ValidateGenesis validates the provided validator genesis state to ensure the
// expected invariants holds. (i.e. no duplicate validators)
func ValidateGenesis(data GenesisState) error {
	if len(data.RootCert) == 0 {
		return errors.New("root certificate is not set in genesis state")
	}

	rootCert, err := cautil.ReadCertificateFromMem([]byte(data.RootCert))
	if err != nil {
		return fmt.Errorf("invalid root certificate in genesis state, %s", err.Error())
	}

	err = validateGenesisStateValidators(rootCert, data.Validators)
	if err != nil {
		return err
	}

	return validateNodes(data.Nodes)
}

func validateGenesisStateValidators(rootCert cautil.Cert, validators []Validator) error {
	nameMap := make(map[string]bool, len(validators))
	pubkeyMap := make(map[string]bool, len(validators))
	idMap := make(map[string]bool, len(validators))

	for i := 0; i < len(validators); i++ {
		val := validators[i]
		if len(val.Certificate) > 0 {
			cert, err := cautil.ReadCertificateFromMem([]byte(val.Certificate))
			if err != nil {
				return sdkerrors.Wrap(types.ErrInvalidCert, err.Error())
			}

			if err = cautil.VerifyCertFromRoot(cert, rootCert); err != nil {
				return sdkerrors.Wrapf(types.ErrInvalidCert, "cannot be verified by root certificate, err: %s", err.Error())
			}
		}

		if _, ok := nameMap[val.Id]; ok {
			return fmt.Errorf("duplicate validator id in genesis state: ID %v, pubkey %v", val.Id, val.Pubkey)
		}

		if _, ok := idMap[val.Name]; ok {
			return fmt.Errorf("duplicate validator name in genesis state: ID %v, pubkey %v", val.Id, val.Pubkey)
		}

		if _, ok := pubkeyMap[val.Pubkey]; ok {
			return fmt.Errorf("duplicate validator pubkey in genesis state: ID %v, pubkey %v", val.Id, val.Pubkey)
		}

		if val.Jailed {
			return fmt.Errorf("validator is jailed in genesis state: name %v, ID %v", val.Id, val.Pubkey)
		}

		pubkeyMap[val.Pubkey] = true
		nameMap[val.Name] = true
		idMap[val.Id] = true
	}

	return nil
}

// validateNodes validates the nodes in genesis state
func validateNodes(nodes []types.Node) error {
	for _, node := range nodes {
		if err := node.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func newTmCryptoCodec() *codec.LegacyAmino {
	cdc := codec.NewLegacyAmino()
	cdc.RegisterInterface((*crypto.PubKey)(nil), nil)
	cdc.RegisterConcrete(sm2.PubKeySm2{}, sm2.PubKeyName, nil)
	cdc.RegisterConcrete(sr25519.PubKey{}, sr25519.PubKeyName, nil)
	cdc.RegisterConcrete(ed25519.PubKey{}, ed25519.PubKeyName, nil)
	cdc.RegisterConcrete(secp256k1.PubKey{}, secp256k1.PubKeyName, nil)
	return cdc
}

func TmPk2ProtoPk(tmPubKey string, cdc *codec.LegacyAmino) string {
	bz, err := sdk.GetFromBech32(tmPubKey, sdk.GetConfig().GetBech32ConsensusPubPrefix())
	var tmpk crypto.PubKey
	e := cdc.Unmarshal(bz, &tmpk)
	if e != nil {
		panic(e)
	}
	pk, err := cryptocodec.FromTmPubKeyInterface(tmpk)
	if err != nil {
		panic(err)
	}
	pkByte, err := legacy.Cdc.Marshal(pk)
	if err != nil {
		panic(err)
	}
	keyStr, err := sdk.Bech32ifyAddressBytes(sdk.GetConfig().GetBech32ConsensusPubPrefix(), pkByte)
	if err != nil {
		panic(err)
	}
	return keyStr
}
