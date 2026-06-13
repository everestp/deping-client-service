use anchor_lang::prelude::*;
use anchor_lang::solana_program::pubkey;
use anchor_spl::token::{self, Token, TokenAccount, Transfer};

declare_id!("DVicVozhh4y38dA6iCzfPp2c4xj5Q29mJq6HgF5Eufiz");

// ================================================================================================================
//                                         GLOBAL PLATFORM CONSTANTS
// ================================================================================================================
pub const ADMIN_WALLET: Pubkey = pubkey!("J81HEGiTH1eJNPV9bvoHNEejCDxpz8GSVLUyciuuFmoy");
pub const BACKEND_WALLET: Pubkey = pubkey!("2fxkwit97yvCRtFEaokiqgAb7tgSQLp8iXcburG6vh2H");
pub const DEEPING_MINT: Pubkey = pubkey!("DPg3P2U4syj8eGL6rRqMqhUfDayxVunh7Fmcowwh6hsj");
pub const MINIMUM_VALIDATOR_STAKE: u64 = 20_000_000_000; 
pub const UNSTAKE_COOLDOWN_SECONDS: i64 = 604_800;

// ===============================================================================================================
//                                        CONTEXT INJECTION MECHANICS
// ================================================================================================================

#[derive(Accounts)]
#[instruction(email_hash: [u8; 32])] 
pub struct InitNode<'info> {
    #[account(
        init, 
        payer = owner, 
        space = 8 + 32 + 32 + 8 + 8 + 8 + 1 + 1, 
        seeds = [b"node", owner.key().as_ref(), &email_hash], 
        bump
    )]
    pub node_account: Account<'info, NodeAccount>,
    #[account(mut)]
    pub owner: Signer<'info>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct AddReward<'info> {
    #[account(mut)]
    pub node_account: Account<'info, NodeAccount>,
    #[account(constraint = signer.key() == BACKEND_WALLET @ ErrorCode::Unauthorized)]
    pub signer: Signer<'info>,
}

#[derive(Accounts)]
pub struct ClaimReward<'info> {
    #[account(mut)]
    pub node_account: Account<'info, NodeAccount>,
    #[account(mut)]
    pub treasury_token_account: Account<'info, TokenAccount>,
    #[account(mut)]
    pub user_token_account: Account<'info, TokenAccount>,
    pub treasury_authority: AccountInfo<'info>,
    pub token_program: Program<'info, Token>,
}

#[derive(Accounts)]
pub struct BuyProduct<'info> {
    #[account(mut)]
    pub user_token_account: Account<'info, TokenAccount>,
    #[account(mut)]
    pub treasury_token_account: Account<'info, TokenAccount>,
    pub user: Signer<'info>,
    pub token_program: Program<'info, Token>,
}

#[derive(Accounts)]
pub struct WithdrawTreasury<'info> {
    #[account(mut)]
    pub treasury_token_account: Account<'info, TokenAccount>,
    #[account(mut)]
    pub admin_token_account: Account<'info, TokenAccount>,
    pub treasury_authority: AccountInfo<'info>,
    #[account(constraint = admin.key() == ADMIN_WALLET @ ErrorCode::Unauthorized)]
    pub admin: Signer<'info>,
    pub token_program: Program<'info, Token>,
}

#[derive(Accounts)]
pub struct StakeTokens<'info> {
    #[account(mut)]
    pub node_account: Account<'info, NodeAccount>,
    #[account(mut)]
    pub user_token_account: Account<'info, TokenAccount>,
    #[account(mut)]
    pub staking_vault: Account<'info, TokenAccount>,
    pub owner: Signer<'info>,
    pub token_program: Program<'info, Token>,
}

#[derive(Accounts)]
pub struct RequestUnstake<'info> {
    #[account(mut, has_one = owner)]
    pub node_account: Account<'info, NodeAccount>,
    pub owner: Signer<'info>,
}

#[derive(Accounts)]
pub struct WithdrawStake<'info> {
    #[account(mut, has_one = owner)]
    pub node_account: Account<'info, NodeAccount>,
    #[account(mut)]
    pub user_token_account: Account<'info, TokenAccount>,
    #[account(mut)]
    pub staking_vault: Account<'info, TokenAccount>,
    pub staking_vault_authority: AccountInfo<'info>,
    pub owner: Signer<'info>,
    pub token_program: Program<'info, Token>,
}

// ================================================================================================================
//                                         PROGRAM LOGIC
// ================================================================================================================

#[program]
pub mod deping {
    use super::*;

    pub fn init_node(ctx: Context<InitNode>, email_hash: [u8; 32]) -> Result<()> {
        let node = &mut ctx.accounts.node_account;
        node.owner = ctx.accounts.owner.key();
        node.email_hash = email_hash;
        node.reward_balance = 0;
        node.staked_amount = 0;
        node.unstake_request_at = 0;
        node.is_validator = false;
        node.bump = ctx.bumps.node_account;
        Ok(())
    }

    pub fn add_reward(ctx: Context<AddReward>, amount: u64) -> Result<()> {
        let node = &mut ctx.accounts.node_account;
        node.reward_balance = node.reward_balance.checked_add(amount).ok_or(ErrorCode::Overflow)?;
        Ok(())
    }

    pub fn claim_reward(ctx: Context<ClaimReward>, amount: u64) -> Result<()> {
        let node = &mut ctx.accounts.node_account;
        require!(amount > 0, ErrorCode::InvalidAmount);
        require!(node.reward_balance >= amount, ErrorCode::InsufficientFunds);
        node.reward_balance = node.reward_balance.checked_sub(amount).ok_or(ErrorCode::Overflow)?;

        let (_authority, bump) = Pubkey::find_program_address(&[b"treasury"], ctx.program_id);
        let bump_seed = [bump];
        let seeds: &[&[u8]] = &[b"treasury", &bump_seed];
        let signer = &[seeds];

        token::transfer(
            CpiContext::new_with_signer(ctx.accounts.token_program.to_account_info(), Transfer {
                from: ctx.accounts.treasury_token_account.to_account_info(),
                to: ctx.accounts.user_token_account.to_account_info(),
                authority: ctx.accounts.treasury_authority.to_account_info(),
            }, signer),
            amount,
        )?;
        Ok(())
    }

    pub fn buy_product(ctx: Context<BuyProduct>, amount: u64) -> Result<()> {
        require!(amount > 0, ErrorCode::InvalidAmount);
        token::transfer(
            CpiContext::new(ctx.accounts.token_program.to_account_info(), Transfer {
                from: ctx.accounts.user_token_account.to_account_info(),
                to: ctx.accounts.treasury_token_account.to_account_info(),
                authority: ctx.accounts.user.to_account_info(),
            }),
            amount,
        )?;
        Ok(())
    }

    pub fn withdraw_treasury(ctx: Context<WithdrawTreasury>, amount: u64) -> Result<()> {
        let (_authority, bump) = Pubkey::find_program_address(&[b"treasury"], ctx.program_id);
        let bump_seed = [bump];
        let seeds: &[&[u8]] = &[b"treasury", &bump_seed];
        let signer = &[seeds];

        token::transfer(
            CpiContext::new_with_signer(ctx.accounts.token_program.to_account_info(), Transfer {
                from: ctx.accounts.treasury_token_account.to_account_info(),
                to: ctx.accounts.admin_token_account.to_account_info(),
                authority: ctx.accounts.treasury_authority.to_account_info(),
            }, signer),
            amount,
        )?;
        Ok(())
    }

    pub fn stake_tokens(ctx: Context<StakeTokens>, amount: u64) -> Result<()> {
        let node = &mut ctx.accounts.node_account;
        require!(!node.is_validator && node.unstake_request_at == 0, ErrorCode::UnstakeCooldownActive);
        node.staked_amount = node.staked_amount.checked_add(amount).ok_or(ErrorCode::Overflow)?;
        require!(node.staked_amount >= MINIMUM_VALIDATOR_STAKE, ErrorCode::InsufficientStake);
        node.is_validator = true;

        token::transfer(
            CpiContext::new(ctx.accounts.token_program.to_account_info(), Transfer {
                from: ctx.accounts.user_token_account.to_account_info(),
                to: ctx.accounts.staking_vault.to_account_info(),
                authority: ctx.accounts.owner.to_account_info(),
            }),
            amount,
        )?;
        Ok(())
    }

  // Use this to add more stake at any time (Top-Up)
    pub fn add_stake(ctx: Context<StakeTokens>, amount: u64) -> Result<()> {
        let node = &mut ctx.accounts.node_account;
        
        node.staked_amount = node.staked_amount.checked_add(amount).ok_or(ErrorCode::Overflow)?;
        
        if node.staked_amount >= MINIMUM_VALIDATOR_STAKE {
            node.is_validator = true;
        }

        token::transfer(
            CpiContext::new(
                ctx.accounts.token_program.to_account_info(),
                Transfer {
                    from: ctx.accounts.user_token_account.to_account_info(),
                    to: ctx.accounts.staking_vault.to_account_info(),
                    authority: ctx.accounts.owner.to_account_info(),
                },
            ),
            amount,
        )?;
        Ok(())
    }

    // Atomic: Withdraws exact amount, checks minimums, updates status
    pub fn withdraw_stake(ctx: Context<WithdrawStake>, amount: u64) -> Result<()> {
        let node = &mut ctx.accounts.node_account;
        require!(node.staked_amount >= amount, ErrorCode::InsufficientFunds);
        
        let remaining = node.staked_amount.checked_sub(amount).ok_or(ErrorCode::Overflow)?;

        if remaining > 0 {
            require!(remaining >= MINIMUM_VALIDATOR_STAKE, ErrorCode::InsufficientStake);
        } else {
            node.is_validator = false;
        }

        node.staked_amount = remaining;

        let (_authority, bump) = Pubkey::find_program_address(&[b"staking_vault"], ctx.program_id);
        let seeds: &[&[u8]] = &[b"staking_vault", &[bump]];
        let signer = &[seeds];

        token::transfer(
            CpiContext::new_with_signer(
                ctx.accounts.token_program.to_account_info(),
                Transfer {
                    from: ctx.accounts.staking_vault.to_account_info(),
                    to: ctx.accounts.user_token_account.to_account_info(),
                    authority: ctx.accounts.staking_vault_authority.to_account_info(),
                },
                signer,
            ),
            amount,
        )?;
        Ok(())
    }

    // Atomic: Cleans up vault, wipes PDA, transfers SOL rent back to user
    pub fn delete_account(ctx: Context<DeleteAccount>, amount: u64) -> Result<()> {
        let node = &mut ctx.accounts.node_account;
        require!(node.staked_amount == amount, ErrorCode::InsufficientFunds);

        if amount > 0 {
            let (_authority, bump) = Pubkey::find_program_address(&[b"staking_vault"], ctx.program_id);
            let seeds: &[&[u8]] = &[b"staking_vault", &[bump]];
            let signer = &[seeds];

            token::transfer(
                CpiContext::new_with_signer(
                    ctx.accounts.token_program.to_account_info(),
                    Transfer {
                        from: ctx.accounts.staking_vault.to_account_info(),
                        to: ctx.accounts.user_token_account.to_account_info(),
                        authority: ctx.accounts.staking_vault_authority.to_account_info(),
                    },
                    signer,
                ),
                amount,
            )?;
        }
        Ok(())
    }
}

#[derive(Accounts)]
pub struct DeleteAccount<'info> {
    #[account(
        mut, 
        has_one = owner,
        constraint = node_account.reward_balance == 0,
        close = owner // <--- Automatically wipes PDA data and transfers rent to user
    )]
    pub node_account: Account<'info, NodeAccount>,
    #[account(mut)]
    pub staking_vault: Account<'info, TokenAccount>,
    #[account(mut)]
    pub user_token_account: Account<'info, TokenAccount>,
    pub staking_vault_authority: AccountInfo<'info>,
    #[account(mut)]
    pub owner: Signer<'info>,
    pub token_program: Program<'info, Token>,
}

#[account]
pub struct NodeAccount {
    pub owner: Pubkey,
    pub email_hash: [u8; 32],
    pub reward_balance: u64,
    pub staked_amount: u64,
    pub unstake_request_at: i64,
    pub is_validator: bool,
    pub bump: u8,
}

#[error_code]
pub enum ErrorCode {
    #[msg("Administrative or signature execution context unauthorized.")]
    Unauthorized,
    #[msg("Provided transaction amount must be greater than zero.")]
    InvalidAmount,
    #[msg("Requested withdrawal value exceeds verified on-chain node metrics balance.")]
    InsufficientFunds,
    #[msg("Mathematical calculation triggered an overflow/underflow state boundary.")]
    Overflow,
    #[msg("Staking allocation size does not satisfy the network structural minimum.")]
    InsufficientStake,
    #[msg("Unstaking cooldown period remains active or has not been initiated.")]
    UnstakeCooldownActive,
    #[msg("Staking withdrawal request denied due to unverified cooldown sequences.")]
    NotAuthorizedToUnstake,
}