use crate::Error;
use std::sync::{
    Arc,
    atomic::{AtomicUsize, Ordering},
};

pub(crate) const MIB: usize = 1024 * 1024;
pub(crate) const FILE_BYTES: usize = 16 * MIB;
pub(crate) const OUTPUT_BYTES: usize = 64 * MIB;
pub(crate) const FILES: usize = 20_000;
pub(crate) const ROWS: usize = 200_000;
pub(crate) const HUNKS: usize = 5_000;
pub(crate) const SNAPSHOT_BYTES: usize = 256 * MIB;

#[derive(Clone)]
pub(crate) struct Budget(Arc<AtomicUsize>);

impl Budget {
    pub fn new() -> Self {
        Self(Arc::new(AtomicUsize::new(0)))
    }

    pub fn reserve(&self, bytes: usize) -> Result<Reservation, Error> {
        self.0
            .fetch_update(Ordering::AcqRel, Ordering::Acquire, |used| {
                used.checked_add(bytes).filter(|&total| total <= 368 * MIB)
            })
            .map_err(|_| Error::Budget("memory"))?;
        Ok(Reservation {
            budget: self.clone(),
            bytes,
        })
    }
}

pub(crate) struct Reservation {
    budget: Budget,
    bytes: usize,
}

impl Drop for Reservation {
    fn drop(&mut self) {
        let previous = self.budget.0.fetch_sub(self.bytes, Ordering::AcqRel);
        assert!(previous >= self.bytes, "reservation released twice");
    }
}

pub(crate) struct Bytes {
    pub data: Vec<u8>,
    _reservation: Reservation,
}

impl Bytes {
    pub fn new(budget: &Budget, capacity: usize) -> Result<Self, Error> {
        let reservation = budget.reserve(capacity)?;
        let mut data = Vec::new();
        data.try_reserve_exact(capacity)
            .map_err(|_| Error::Budget("allocation"))?;
        Ok(Self {
            data,
            _reservation: reservation,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    #[test]
    fn retained_generations_share_admission_and_drop_releases_it() {
        let budget = Budget::new();
        let old = budget.reserve(200 * MIB).unwrap();
        assert!(matches!(
            budget.reserve(200 * MIB),
            Err(Error::Budget("memory"))
        ));
        drop(old);
        assert!(budget.reserve(368 * MIB).is_ok());
    }
}
