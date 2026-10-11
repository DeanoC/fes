// SPDX-License-Identifier: GPL-2.0-or-later
// Hatari - sound.c: GNU General Public License version2 or any later version.
// Upstream credits: Arnaud Carre (StSound), Benjamin Gerard (Sc68), Antti
// Lankila (YM interpolation); cycle-exact YM rewrite by Nicolas Pomarede.
// Independent primary counter/gate oracle: unchanged definitions and tables
// from Hatari v2.5.0 src/sound.c (license retained in ../data/GPL-2.0.txt).
// https://github.com/hatari/hatari/blob/v2.5.0/src/sound.c
// Primary SHA256: 2ffdf73c27c82edd998c41a250cd424a8da4f7385d8059f21d36bdc49c99c4f0
// Only minimal types/globals and identity ymout5/LPF_NONE adapters are supplied.
// Identity output exposes gated five-bit A/B/C BEFORE DAC/mixer/filter.
// Reset helper explicitly initializes divider phase and buffer position; test
// cases declare the analytic Galois/Fibonacci state mapping separately.
#ifndef ST_YM_COUNTER_PRIMARY_HPP
#define ST_YM_COUNTER_PRIMARY_HPP
#include <cstdint>
#include <cstring>
namespace st_ym_counter_primary {
using ymu8=uint8_t;using ymu16=uint16_t;using ymu32=uint32_t;
using ymsample=int16_t;
#define YM_MERGE_VOICE(C,B,A) ((C)<<10|(B)<<5|(A))
#define YM_MASK_1VOICE 0x1f
#define YM_MASK_A 0x1f
#define YM_MASK_B (0x1f<<5)
#define YM_MASK_C (0x1f<<10)
#define YM_SQUARE_UP 0x1f
#define YM_SQUARE_DOWN 0
#define YM_BUFFER_250_SIZE 32768
#define YM_BUFFER_250_SIZE_MASK (YM_BUFFER_250_SIZE-1)
#define YM2149_LPF_FILTER_LPF_STF 1
#define YM2149_LPF_FILTER_PWM 2
static ymu16 ToneA_per,ToneA_count,ToneA_val,ToneB_per,ToneB_count,ToneB_val,ToneC_per,ToneC_count,ToneC_val;
static ymu16 Noise_per,Noise_count,Noise_val,Env_per,Env_count;
static ymu32 Env_pos;static int Env_shape;
static ymu32 mixerTA,mixerTB,mixerTC,mixerNA,mixerNB,mixerNC,RndRack;
static ymu16 EnvMask3Voices,Vol3Voices;
static uint8_t SoundRegs[14];static bool bEnvelopeFreqFlag;
static ymu16 YM2149_Freq_div_2;
static ymsample identity[32768],*ymout5=identity;
static int YM2149_LPF_Filter=0;
static ymsample YM_Buffer_250[YM_BUFFER_250_SIZE];
static int YM_Buffer_250_pos_write;
static ymsample LowPassFilter(ymsample x){return x;}
static ymsample PWMaliasFilter(ymsample x){return x;}
// Literal definitions from pinned Hatari v2.5.0 sound.c, GPL-2.0-or-later.
#define	ENV_GODOWN	0		/* 31 ->  0 */
#define	ENV_GOUP	1		/*  0 -> 31 */
#define	ENV_DOWN	2		/*  0 ->  0 */
#define	ENV_UP		3		/* 31 -> 31 */

/* To generate an envelope, we first use block 0, then we repeat blocks 1 and 2 */
static const int YmEnvDef[ 16 ][ 3 ] = {
	{ ENV_GODOWN,	ENV_DOWN, ENV_DOWN } ,		/* 0 \___ */
	{ ENV_GODOWN,	ENV_DOWN, ENV_DOWN } ,		/* 1 \___ */
	{ ENV_GODOWN,	ENV_DOWN, ENV_DOWN } ,		/* 2 \___ */
	{ ENV_GODOWN,	ENV_DOWN, ENV_DOWN } ,		/* 3 \___ */
	{ ENV_GOUP,	ENV_DOWN, ENV_DOWN } ,		/* 4 /___ */
	{ ENV_GOUP,	ENV_DOWN, ENV_DOWN } ,		/* 5 /___ */
	{ ENV_GOUP,	ENV_DOWN, ENV_DOWN } ,		/* 6 /___ */
	{ ENV_GOUP,	ENV_DOWN, ENV_DOWN } ,		/* 7 /___ */
	{ ENV_GODOWN,	ENV_GODOWN, ENV_GODOWN } ,	/* 8 \\\\ */
	{ ENV_GODOWN,	ENV_DOWN, ENV_DOWN } ,		/* 9 \___ */
	{ ENV_GODOWN,	ENV_GOUP, ENV_GODOWN } ,	/* A \/\/ */
	{ ENV_GODOWN,	ENV_UP, ENV_UP } ,		/* B \--- */
	{ ENV_GOUP,	ENV_GOUP, ENV_GOUP } ,		/* C //// */
	{ ENV_GOUP,	ENV_UP, ENV_UP } ,		/* D /--- */
	{ ENV_GOUP,	ENV_GODOWN, ENV_GOUP } ,	/* E /\/\ */
	{ ENV_GOUP,	ENV_DOWN, ENV_DOWN } ,		/* F /___ */
	};


/* Buffer to store the 16 envelopes built from YmEnvDef */
static ymu16	YmEnvWaves[ 16 ][ 32 * 3 ];		/* 16 envelopes with 3 blocks of 32 volumes */




static const ymu16 YmVolume4to5[ 16 ] = { 0,1,5,7,9,11,13,15,17,19,21,23,25,27,29,31 };

static void	YM2149_EnvBuild ( void )
{
	int	env;
	int	block;
	int	vol=0 , inc=0;
	int	i;


	for ( env=0 ; env<16 ; env++ )				/* 16 possible envelopes */
		for ( block=0 ; block<3 ; block++ )		/* 3 blocks to define an envelope */
		{
			switch ( YmEnvDef[ env ][ block ] )
			{
				case ENV_GODOWN :	vol=31 ; inc=-1 ; break;
				case ENV_GOUP :		vol=0  ; inc=1 ; break;
				case ENV_DOWN :		vol=0  ; inc=0 ; break;
				case ENV_UP :		vol=31 ; inc=0 ; break;
			}

			for ( i=0 ; i<32 ; i++ )		/* 32 volumes per block */
			{
				YmEnvWaves[ env ][ block*32 + i ] = YM_MERGE_VOICE ( vol , vol , vol );
				vol += inc;
			}
		}
}

static ymu32	YM2149_RndCompute(void)
{
	/*  17 stage, 2 taps (17, 14) LFSR */
	if (RndRack & 1)
	{
		RndRack = RndRack>>1 ^ 0x12000;		/* bits 17 and 14 are ones */
		return 0xffff;
	}
	else
	{	RndRack >>= 1;
		return 0;
	}
}

static ymu16	YM2149_TonePer(ymu8 rHigh , ymu8 rLow)
{
	ymu16	per;

	per = ( ( rHigh & 0x0f ) << 8 ) + rLow;
	return per;
}

static ymu16	YM2149_NoisePer(ymu8 rNoise)
{
	ymu16	per;

	per = rNoise & 0x1f;
	return per;
}

static ymu16	YM2149_EnvPer(ymu8 rHigh , ymu8 rLow)
{
	ymu16	per;

	per = ( rHigh << 8 ) + rLow;
	return per;
}

void Sound_WriteReg(int reg, uint8_t data)
{
	switch (reg)
	{
		case 0:
			SoundRegs[0] = data;
			ToneA_per = YM2149_TonePer ( SoundRegs[1] , SoundRegs[0] );
			break;
		case 1:
			SoundRegs[1] = data & 0x0f;
			ToneA_per = YM2149_TonePer ( SoundRegs[1] , SoundRegs[0] );
			break;
		case 2:
			SoundRegs[2] = data;
			ToneB_per = YM2149_TonePer ( SoundRegs[3] , SoundRegs[2] );
			break;
		case 3:
			SoundRegs[3] = data & 0x0f;
			ToneB_per = YM2149_TonePer ( SoundRegs[3] , SoundRegs[2] );
			break;
		case 4:
			SoundRegs[4] = data;
			ToneC_per = YM2149_TonePer ( SoundRegs[5] , SoundRegs[4] );
			break;
		case 5:
			SoundRegs[5] = data & 0x0f;
			ToneC_per = YM2149_TonePer ( SoundRegs[5] , SoundRegs[4] );
			break;
		case 6:
			SoundRegs[6] = data & 0x1f;
			Noise_per = YM2149_NoisePer ( SoundRegs[6] );
			break;

		case 7:
			SoundRegs[7] = data & 0x3f;			/* ignore bits 6 and 7 */
			mixerTA = (data&(1<<0)) ? 0xffff : 0;
			mixerTB = (data&(1<<1)) ? 0xffff : 0;
			mixerTC = (data&(1<<2)) ? 0xffff : 0;
			mixerNA = (data&(1<<3)) ? 0xffff : 0;
			mixerNB = (data&(1<<4)) ? 0xffff : 0;
			mixerNC = (data&(1<<5)) ? 0xffff : 0;
			break;

		case 8:
			SoundRegs[8] = data & 0x1f;
			if ( data & 0x10 )
			{
				EnvMask3Voices |= YM_MASK_A;		/* env ON */
				Vol3Voices &= ~YM_MASK_A;		/* fixed vol OFF */
			}
			else
			{
				EnvMask3Voices &= ~YM_MASK_A;		/* env OFF */
				Vol3Voices &= ~YM_MASK_A;		/* clear previous vol */
				Vol3Voices |= YmVolume4to5[ SoundRegs[8] ];	/* fixed vol ON */
			}
			break;

		case 9:
			SoundRegs[9] = data & 0x1f;
			if ( data & 0x10 )
			{
				EnvMask3Voices |= YM_MASK_B;		/* env ON */
				Vol3Voices &= ~YM_MASK_B;		/* fixed vol OFF */
			}
			else
			{
				EnvMask3Voices &= ~YM_MASK_B;		/* env OFF */
				Vol3Voices &= ~YM_MASK_B;		/* clear previous vol */
				Vol3Voices |= ( YmVolume4to5[ SoundRegs[9] ] ) << 5;	/* fixed vol ON */
			}
			break;

		case 10:
			SoundRegs[10] = data & 0x1f;
			if ( data & 0x10 )
			{
				EnvMask3Voices |= YM_MASK_C;		/* env ON */
				Vol3Voices &= ~YM_MASK_C;		/* fixed vol OFF */
			}
			else
			{
				EnvMask3Voices &= ~YM_MASK_C;		/* env OFF */
				Vol3Voices &= ~YM_MASK_C;		/* clear previous vol */
				Vol3Voices |= ( YmVolume4to5[ SoundRegs[10] ] ) << 10;	/* fixed vol ON */
			}
			break;

		case 11:
			SoundRegs[11] = data;
			Env_per = YM2149_EnvPer ( SoundRegs[12] , SoundRegs[11] );
			break;

		case 12:
			SoundRegs[12] = data;
			Env_per = YM2149_EnvPer ( SoundRegs[12] , SoundRegs[11] );
			break;

		case 13:
			SoundRegs[13] = data & 0xf;
			Env_pos = 0;					/* when writing to Env_shape, we must reset the Env_pos */
			Env_count = 0;					/* this also starts a new phase */
			Env_shape = SoundRegs[13];
			bEnvelopeFreqFlag = true;			/* used for YmFormat saving */
			break;

	}
}

static void	Ym2149_Reset(void)
{
	int	i;

	for ( i=0 ; i<14 ; i++ )
		Sound_WriteReg ( i , 0 );

	Sound_WriteReg ( 7 , 0xff );

	/* Reset internal variables and counters */
	ToneA_per = ToneA_count = 0;
	ToneB_per = ToneB_count = 0;
	ToneC_per = ToneC_count = 0;
	Noise_per = Noise_count = 0;
	Env_per = Env_count = 0;
	Env_shape = Env_pos = 0;

	ToneA_val = ToneB_val = ToneC_val = Noise_val = YM_SQUARE_DOWN;

	RndRack = 1;
}

static void	YM2149_DoSamples_250 ( int SamplesToGenerate_250 )
{
	ymsample	sample;
	ymu32		bt;
	ymu16		Env3Voices;			/* 0x00CCBBAA */
	ymu16		Tone3Voices;			/* 0x00CCBBAA */
	int		pos;
	int		n;


//fprintf ( stderr , "ym2149_dosamples_250 in nb=%d ym_pos_wr=%d\n",SamplesToGenerate_250 , YM_Buffer_250_pos_write );

	/* We write new samples at position YM_Buffer_250_pos_write while we read them at the same time */
	/* at position YM_Buffer_250_pos_read (to create the output at YM_REPLAY_FREQ) */
	/* This means we must ensure YM_Buffer_250[] is large enough to avoid overwriting data */
	/* that are not read yet */
	pos = YM_Buffer_250_pos_write;

	/* Emulate as many internal YM cycles as needed to generate samples */
	for ( n=0 ; n<SamplesToGenerate_250 ; n++ )
	{
		/* Emulate 1 internal YM2149 cycle : increase all counters */
		/* As measured on a real YM2149, result for per==0 is the same as for per==1 */
		/* To obtain this in our code, counters are incremented first, then compared to per, */
		/* which gives the same result when per=1 and when per=0 */

		/* Special case for noise counter, it's increased at 125 KHz, not 250 KHz */
		YM2149_Freq_div_2 ^= 1;
		if ( YM2149_Freq_div_2 == 0 )
			Noise_count++;
		if ( Noise_count >= Noise_per )
		{
			Noise_count = 0;
			Noise_val = YM2149_RndCompute();/* 0 or 0xffff */
		}

//fprintf ( stderr , "ym2149_dosamples_250 max=%d n=%d ToneA_count=%d ToneA_per=%d val=%x pos=%d\n",SamplesToGenerate_250,n,ToneA_count,ToneA_per,ToneA_val,pos );
		/* Other counters are increased on every call, at 250 KHz */
		ToneA_count++;
		if ( ToneA_count >= ToneA_per )
		{
//fprintf ( stderr , "ym2149_dosamples_250 max=%d n=%d ToneA_count=%d ToneA_per=%d val=%x pos=%d toggle\n",SamplesToGenerate_250,n,ToneA_count,ToneA_per,ToneA_val,pos );
			ToneA_count = 0;
			ToneA_val ^= YM_SQUARE_UP;	/* 0 or 0x1f */
		}

		ToneB_count++;
		if ( ToneB_count >= ToneB_per )
		{
			ToneB_count = 0;
			ToneB_val ^= YM_SQUARE_UP;	/* 0 or 0x1f */
		}

		ToneC_count++;
		if ( ToneC_count >= ToneC_per )
		{
			ToneC_count = 0;
			ToneC_val ^= YM_SQUARE_UP;	/* 0 or 0x1f */
		}

		Env_count += 1;
		if ( Env_count >= Env_per )
		{
			Env_count = 0;
			Env_pos += 1;
			if ( Env_pos >= 3*32 )		/* blocks 0, 1 and 2 were used (Env_pos 0 to 95) */
				Env_pos -= 2*32;	/* replay/loop blocks 1 and 2 (Env_pos 32 to 95) */
		}

		/* Build 'sample' value with the values of tone/noise/volume/env */

		/* Get the 5 bits volume corresponding to the current envelope's position */
		Env3Voices = YmEnvWaves[ Env_shape ][ Env_pos ];
		Env3Voices &= EnvMask3Voices;			/* only keep volumes for voices using envelope */

		/* Tone3Voices will contain the output state of each voice : 0 or 0x1f */
		bt = (ToneA_val | mixerTA) & (Noise_val | mixerNA);	/* 0 or 0xffff */
		Tone3Voices = bt & YM_MASK_1VOICE;		/* 0 or 0x1f */

		bt = (ToneB_val | mixerTB) & (Noise_val | mixerNB);
		Tone3Voices |= ( bt & YM_MASK_1VOICE ) << 5;

		bt = (ToneC_val | mixerTC) & (Noise_val | mixerNC);
		Tone3Voices |= ( bt & YM_MASK_1VOICE ) << 10;

		/* Combine fixed volumes and envelope volumes and keep the resulting */
		/* volumes depending on the output state of each voice (0 or 0x1f) */
		Tone3Voices &= ( Env3Voices | Vol3Voices );

		sample = ymout5[ Tone3Voices ];			/* 16 bits signed value */

		/* Apply low pass filter ? */
		if ( YM2149_LPF_Filter == YM2149_LPF_FILTER_LPF_STF )
			sample = LowPassFilter ( sample );
		else if ( YM2149_LPF_Filter == YM2149_LPF_FILTER_PWM )
			sample = PWMaliasFilter ( sample );

		/* Store sample */
		YM_Buffer_250[ pos ] = sample;
		pos = ( pos + 1 ) & YM_BUFFER_250_SIZE_MASK;
	}


#ifdef YM_250_DEBUG
	/* write raw 250 kHz samples into a wav file */
	YM2149_DoSamples_250_Debug ( SamplesToGenerate_250 , YM_Buffer_250_pos_write );
#endif

	YM_Buffer_250_pos_write = pos;

//fprintf ( stderr , "ym2149_dosamples_250 out nb=%d ym_pos_wr=%d\n",SamplesToGenerate_250 , YM_Buffer_250_pos_write );
}


static void primary_reset(){
 YM2149_EnvBuild();for(int i=0;i<32768;++i)identity[i]=i;
 Ym2149_Reset();YM2149_Freq_div_2=0;YM_Buffer_250_pos_write=0;
}
static unsigned primary_tick(){int p=YM_Buffer_250_pos_write;YM2149_DoSamples_250(1);return uint16_t(YM_Buffer_250[p]);}

} // namespace st_ym_counter_primary
#endif
