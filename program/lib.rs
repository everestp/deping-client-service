use anchor_lang::prelude::*;
use anchor_lang::solana_program::pubkey;
use anchor_spl::token::{self, Token, TokenAccount, Mint, Transfer};

declare_id!("EA4pKJ33F2p4oQyKNcCGMBptjSgbHQzCz2H8QgHbYAgR");

// ================================================================================================================
//                                         GLOBAL PLATFORM CONSTANTS
// ================================================================================================================

/// Cold Storage Master Wallet (Authorized exclusively for initialization and corporate treasury extraction)
pub const ADMIN_WALLET: Pubkey =
    pubkey!("D5T8XzDrmUpftykvA7yXfvvVqgS2h9wTbiPr5WhFe7Zj");

/// Hot Wallet Key (Maintained on your Go backend service instance strictly for high-frequency reward injections)
pub const BACKEND_WALLET: Pubkey =
    pubkey!("4faW5GHsCXwGgQAMmAL7sSENpaezCb63cncWvzGc8iJa");

/// Platform Core Mint Address (9-Decimal Asset)
pub const DEEPING_MINT: Pubkok ey =
    pubkey!("2V5HdggYQXW1Z9nhrVKjNdYqg5NsQnZhwMERYr8WK1pU");

/// Minimum validation entry limit: 20 tokens assuming 9-decimal precision layout (20 * 10^9)
pub const MINIMUM_VALIDATOR_STAKE: u64 = 20_000_000_000; 

/// Unstaking Cooldown: 7 Days (604,800 Seconds)
pub const UNSTAKE_COOLDOWN_SECONDS: i64 = 604_800;

#[program]
pub mod deping {
    use super::*;

    /// Initializes a tracking node PDA. Executed and paid for by the Go Backend hot key.
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

    /// Hardened Off-Chain Batch Synchronizer. Invoked strictly by the authorized Go backend hot wallet.
    pub fn add_reward(ctx: Context<AddReward>, amount: u64) -> Result<()> {
        let node = &mut ctx.accounts.node_account;
        node.reward_balance = node.reward_balance
            .checked_add(amount)
            .ok_or(ErrorCode::Overflow)?;
        Ok(())
    }

    /// Pulls synced rewards from the platform PDA escrow vault directly into user wallets.
    pub fn claim_reward(ctx: Context<ClaimReward>, amount: u64) -> Result<()> {
        let node = &mut ctx.accounts.node_account;
        
        require!(amount > 0, ErrorCode::InvalidAmount);
        require!(node.reward_balance >= amount, ErrorCode::InsufficientFunds);

        // State mutation occurs BEFORE external CPI call to eliminate reentrancy attacks
        node.reward_balance = node.reward_balance
            .checked_sub(amount)
            .ok_or(ErrorCode::Overflow)?;

        let seeds: &[&[u8]] = &[b"treasury", &[ctx.bumps.treasury_authority]];
        let signer = &[seeds];

        let cpi_accounts = Transfer {
            from: ctx.accounts.treasury_token_account.to_account_info(),
            to: ctx.accounts.user_token_account.to_account_info(),
            authority: ctx.accounts.treasury_authority.to_account_info(),
        };

        token::transfer(
            CpiContext::new_with_signer(
                ctx.accounts.token_program.to_account_info(),
                cpi_accounts,
                signer,
            ),
            amount,
        )?;
        Ok(())
    }

    /// Locks capital from user wallets up into the treasury vault to pay for data/products.
    pub fn buy_product(ctx: Context<BuyProduct>, amount: u64) -> Result<()> {
        require!(amount > 0, ErrorCode::InvalidAmount);
        
        let cpi_accounts = Transfer {
            from: ctx.accounts.user_token_account.to_account_info(),
            to: ctx.accounts.treasury_token_account.to_account_info(),
            authority: ctx.accounts.user.to_account_info(),
        };
        token::transfer(
            CpiContext::new(ctx.accounts.token_program.to_account_info(), cpi_accounts),
            amount,
        )?;
        Ok(())
    }

    /// Secure platform revenue withdraw system available exclusively to the administration cold storage wallet.
    pub fn withdraw_treasury(ctx: Context<WithdrawTreasury>, amount: u64) -> Result<()> {
        let seeds: &[&[u8]] = &[b"treasury", &[ctx.bumps.treasury_authority]];
        let signer = &[seeds];

        let cpi_accounts = Transfer {
            from: ctx.accounts.treasury_token_account.to_account_info(),
            to: ctx.accounts.admin_token_account.to_account_info(),
            authority: ctx.accounts.treasury_authority.to_account_info(),
        };

        token::transfer(
            CpiContext::new_with_signer(
                ctx.accounts.token_program.to_account_info(),
                cpi_accounts,
                signer,
            ),
            amount,
        )?;
        Ok(())
    }

    // ================================================================================================================
    //                                         STAKING CORE EXTENSIONS
    // ================================================================================================================

    /// Deposits and locks tokens into the secure staking escrow vault to spin up network validation status.
    pub fn stake_tokens(ctx: Context<StakeTokens>, amount: u64) -> Result<()> {
        let node = &mut ctx.accounts.node_account;
        
        // Prevent staking modifications if an active unstake cooldown layout is already running
        require!(!node.is_validator && node.unstake_request_at == 0, ErrorCode::UnstakeCooldownActive);

        // Accumulate potential incoming stake weight allocations
        node.staked_amount = node.staked_amount
            .checked_add(amount)
            .ok_or(ErrorCode::Overflow)?;

        // Enforce structural minimum stake requirements for 9 decimals (Must be >= 20 Tokens)
        require!(node.staked_amount >= MINIMUM_VALIDATOR_STAKE, ErrorCode::InsufficientStake);
        
        // Set validator capability parameters only after satisfying validation rules
        node.is_validator = true;

        // Route transfer CPI over to the staking vault PDA
        let cpi_accounts = Transfer {
            from: ctx.accounts.user_token_account.to_account_info(),
            to: ctx.accounts.staking_vault.to_account_info(),
            authority: ctx.accounts.owner.to_account_info(),
        };

        token::transfer(
            CpiContext::new(ctx.accounts.token_program.to_account_info(), cpi_accounts),
            amount,
        )?;
        Ok(())
    }

    /// Initiates a lockup release cooldown window, stripping validator privileges instantly.
    pub fn request_unstake(ctx: Context<RequestUnstake>) -> Result<()> {
        let node = &mut ctx.accounts.node_account;
        
        require!(node.is_validator, ErrorCode::Unauthorized);
        require!(node.staked_amount > 0, ErrorCode::InvalidAmount);

        // Initiate timelock epoch capture and immediately drop network validator permissions
        node.unstake_request_at = Clock::get()?.unix_timestamp;
        node.is_validator = false;
        
        Ok(())
    }

    /// Releases locked node stake balances back to users after the 7-day window expires.
    pub fn withdraw_stake(ctx: Context<WithdrawStake>) -> Result<()> {
        let node = &mut ctx.accounts.node_account;
        let now = Clock::get()?.unix_timestamp;
        
        // Validate unstake lifecycle sequence state
        require!(node.unstake_request_at > 0, ErrorCode::NotAuthorizedToUnstake);
        
        // Cooldown check: Must exceed 7 days
        let elapsed = now.checked_sub(node.unstake_request_at).ok_or(ErrorCode::Overflow)?;
        require!(elapsed >= UNSTAKE_COOLDOWN_SECONDS, ErrorCode::UnstakeCooldownActive);

        let amount = node.staked_amount;
        require!(amount > 0, ErrorCode::InvalidAmount);

        // Zero out state modifications before invoking critical token payout CPI calls
        node.staked_amount = 0;
        node.unstake_request_at = 0;

        // Signer seeds for Staking Vault Authority PDA
        let seeds: &[&[u8]] = &[b"staking_vault", &[ctx.bumps.staking_vault_authority]];
        let signer = &[seeds];

        let cpi_accounts = Transfer {
            from: ctx.accounts.staking_vault.to_account_info(),
            to: ctx.accounts.user_token_account.to_account_info(),
            authority: ctx.accounts.staking_vault_authority.to_account_info(),
        };

        token::transfer(
            CpiContext::new_with_signer(
                ctx.accounts.token_program.to_account_info(), 
                cpi_accounts, 
                signer
            ),
            amount,
        )?;
        Ok(())
    }
}

// ================================================================================================================
//                                       STRUCTURAL BLOCK STATE BUFFERS
// ================================================================================================================

#[account]
pub struct NodeAccount {
    pub owner: Pubkey,            // 32 bytes
    pub email_hash: [u8; 32],     // 32 bytes (SHA-256 matching Go backend tracking context)
    pub reward_balance: u64,      // 8 bytes
    pub staked_amount: u64,       // 8 bytes (Active staked collateral token balance)
    pub unstake_request_at: i64,  // 8 bytes (Timestamp marking the beginning of the 7-day cooldown)
    pub is_validator: bool,       // 1 byte  (Flag designating network health monitoring capability)
    pub bump: u8,                 // 1 byte
}

// ================================================================================================================
//                                        CONTEXT INJECTION MECHANICS
// ================================================================================================================

#[derive(Accounts)]
#[instruction(email_hash: [u8; 32])]
pub struct InitNode<'info> {
    #[account(
        init,
        payer = authority,
        space = 8 + 32 + 32 + 8 + 8 + 8 + 1 + 1,
        seeds = [
            b"node",
            owner.key().as_ref(),
            &email_hash
        ],
        bump
    )]
    pub node_account: Account<'info, NodeAccount>,

    #[account(mut, constraint = authority.key() == BACKEND_WALLET @ ErrorCode::Unauthorized)]
    pub authority: Signer<'info>, 

    /// CHECK: Target runner base wallet public key
    pub owner: UncheckedAccount<'info>,
    pub system_program: Program<'info, System>,
}

#[derive(Accounts)]
pub struct AddReward<'info> {
    #[account(mut)]
    pub node_account: Account<'info, NodeAccount>,
    #[account(constraint = backend.key() == BACKEND_WALLET @ ErrorCode::Unauthorized)]
    pub backend: Signer<'info>,
}

#[derive(Accounts)]
pub struct ClaimReward<'info> {
    #[account(mut, has_one = owner @ ErrorCode::Unauthorized)]
    pub node_account: Account<'info, NodeAccount>,
    pub owner: Signer<'info>,
    
    #[account(address = DEEPING_MINT)]
    pub mint: Account<'info, Mint>,
    
    #[account(mut, token::mint = mint, token::authority = treasury_authority)]
    pub treasury_token_account: Account<'info, TokenAccount>,
    
    #[account(seeds = [b"treasury"], bump)]
    /// CHECK: Structural PDA signing handle
    pub treasury_authority: UncheckedAccount<'info>,
    
    #[account(mut, token::mint = mint, token::authority = owner)]
    pub user_token_account: Account<'info, TokenAccount>,
    pub token_program: Program<'info, Token>,
}

#[derive(Accounts)]
pub struct BuyProduct<'info> {
    pub user: Signer<'info>,
    #[account(address = DEEPING_MINT)]
    pub mint: Account<'info, Mint>,
    #[account(mut, token::mint = mint, token::authority = user)]
    pub user_token_account: Account<'info, TokenAccount>,
    #[account(mut, token::mint = mint)]
    pub treasury_token_account: Account<'info, TokenAccount>,
    pub token_program: Program<'info, Token>,
}

#[derive(Accounts)]
pub struct WithdrawTreasury<'info> {
    #[account(constraint = admin.key() == ADMIN_WALLET @ ErrorCode::Unauthorized)]
    pub admin: Signer<'info>,
    #[account(address = DEEPING_MINT)]
    pub mint: Account<'info, Mint>,
    #[account(mut, token::mint = mint)]
    pub treasury_token_account: Account<'info, TokenAccount>,
    #[account(seeds = [b"treasury"], bump)]
    /// CHECK: Platform revenue seed authority
    pub treasury_authority: UncheckedAccount<'info>,
    #[account(mut, token::mint = mint)]
    pub admin_token_account: Account<'info, TokenAccount>,
    pub token_program: Program<'info, Token>,
}

#[derive(Accounts)]
pub struct StakeTokens<'info> {
    #[account(mut, has_one = owner @ ErrorCode::Unauthorized)]
    pub node_account: Account<'info, NodeAccount>,
    pub owner: Signer<'info>,
    #[account(mut, token::authority = owner)]
    pub user_token_account: Account<'info, TokenAccount>,
    #[account(mut, token::mint = DEEPING_MINT, token::authority = staking_vault_authority)]
    pub staking_vault: Account<'info, TokenAccount>,
    #[account(seeds = [b"staking_vault"], bump)]
    /// CHECK: Staking vault PDA administrative signer profile handle
    pub staking_vault_authority: UncheckedAccount<'info>,
    pub token_program: Program<'info, Token>,
}

#[derive(Accounts)]
pub struct RequestUnstake<'info> {
    #[account(mut, has_one = owner @ ErrorCode::Unauthorized)]
    pub node_account: Account<'info, NodeAccount>,
    pub owner: Signer<'info>,
}

#[derive(Accounts)]
pub struct WithdrawStake<'info> {
    #[account(mut, has_one = owner @ ErrorCode::Unauthorized)]
    pub node_account: Account<'info, NodeAccount>,
    pub owner: Signer<'info>,
    #[account(mut, token::mint = DEEPING_MINT, token::authority = staking_vault_authority)]
    pub staking_vault: Account<'info, TokenAccount>,
    #[account(seeds = [b"staking_vault"], bump)]
    /// CHECK: PDA for vault authority cryptographic signer handle
    pub staking_vault_authority: UncheckedAccount<'info>,
    #[account(mut, token::authority = owner)]
    pub user_token_account: Account<'info, TokenAccount>,
    pub token_program: Program<'info, Token>,
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